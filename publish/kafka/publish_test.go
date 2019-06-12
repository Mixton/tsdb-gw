package kafka

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/Shopify/sarama"
	"github.com/Shopify/sarama/mocks"
	"github.com/grafana/metrictank/cluster/partitioner"
	"github.com/raintank/schema"
	"github.com/raintank/tsdb-gw/publish/kafka/keycache"
)

func Test_parseTopicSettings(t *testing.T) {
	tests := []struct {
		name                string
		partitionSchemesStr string
		topicsStr           string
		expected            []topicSettings
		wantErr             bool
	}{
		{
			name:                "no_topic",
			partitionSchemesStr: "",
			topicsStr:           "",
			expected:            []topicSettings{},
			wantErr:             true,
		},
		{
			name:                "single_topic",
			partitionSchemesStr: "bySeries",
			topicsStr:           "testTopic",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_same_scheme",
			partitionSchemesStr: "bySeries,bySeries",
			topicsStr:           "testTopic1,testTopic2",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_different_scheme",
			partitionSchemesStr: "bySeries,byOrg",
			topicsStr:           "testTopic1,testTopic2",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_with_spaces",
			partitionSchemesStr: "bySeries  ,byOrg",
			topicsStr:           "testTopic1,  testTopic2",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_shared_scheme",
			partitionSchemesStr: "bySeries",
			topicsStr:           "testTopic1,testTopic2",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			wantErr: false,
		},
		{
			name:                "one_topic_more_schemes_ignored",
			partitionSchemesStr: "bySeries,byOrg",
			topicsStr:           "testTopic",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			wantErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseTopicSettings(test.partitionSchemesStr, test.topicsStr)
			if (err != nil) != test.wantErr {
				t.Errorf("parseTopicSettings() error = %v, wantErr %v", err, test.wantErr)
				return
			}
			for i, topicSetting := range got {
				if topicSetting.name != test.expected[i].name {
					t.Errorf("parseTopicSettings(): incorrect topic name %s, expects %s", topicSetting.name, test.expected[i].name)
				}
				if topicSetting.partitioner.PartitionBy != test.expected[i].partitioner.PartitionBy {
					t.Errorf("parseTopicSettings(): incorrect partition scheme %s, expects %s", topicSetting.partitioner.PartitionBy, test.expected[i].partitioner.PartitionBy)
				}
				if topicSetting.partitioner.Partitioner == nil {
					t.Errorf("parseTopicSettings(): nil partitioner")
				}
			}
		})
	}
}

func Test_Publish(t *testing.T) {
	dataSinglePoint := []*schema.MetricData{
		{
			Name:     "a.b.c",
			OrgId:    1,
			Interval: 10,
		},
	}
	for _, metric := range dataSinglePoint {
		metric.SetId()
	}
	dataManyPoints := []*schema.MetricData{
		{
			Name:     "a.b.c",
			OrgId:    1,
			Interval: 10,
		},
		{
			Name:     "a.b.c.d",
			OrgId:    1,
			Interval: 10,
		},
		{
			Name:     "a.b.c2",
			OrgId:    1,
			Interval: 10,
		},
	}
	for _, metric := range dataManyPoints {
		metric.SetId()
	}

	tests := []struct {
		name    string
		topics  []topicSettings
		data    []*schema.MetricData
		wantErr bool
	}{
		{
			name:    "no_topic",
			topics:  []topicSettings{},
			data:    dataManyPoints,
			wantErr: false,
		},
		{
			name: "single_topic_single_point",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			data:    dataSinglePoint,
			wantErr: false,
		},
		{
			name: "single_topic_many_points",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			data:    dataManyPoints,
			wantErr: false,
		},
		{
			name: "two_topics_single_point",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			data:    dataSinglePoint,
			wantErr: false,
		},
		{
			name: "two_topics_many_points",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
				},
			},
			data:    dataManyPoints,
			wantErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			publisher := mtPublisher{
				autoInterval: false,
				topics:       test.topics,
			}
			mockProducer := mocks.NewSyncProducer(t, nil)
			producer = mockProducer
			keyCache = keycache.NewKeyCache(v2ClearInterval)

			// check that each MetricData is sent once per topic when calling Publish
			for range test.topics {
				for i, _ := range test.data {
					expectedMd := test.data[i]
					mockProducer.ExpectSendMessageWithCheckerFunctionAndSucceed(func(sentData []byte) error {
						expectedData := make([]byte, 0)
						expectedData, err := expectedMd.MarshalMsg(expectedData)
						if err != nil {
							return err
						}
						if bytes.Compare(sentData, expectedData) != 0 {
							sentMd := schema.MetricData{}
							_, err := sentMd.UnmarshalMsg(sentData)
							if err != nil {
								return err
							}
							return errors.New(fmt.Sprintf("Message sent different from expected: %v != %v", sentMd, expectedMd))
						}
						return nil
					})
				}
			}
			err := publisher.Publish(test.data)
			if (err != nil) != test.wantErr {
				t.Errorf("Publish() error = %v, wantErr %v", err, test.wantErr)
				return
			}
		})
	}

}
