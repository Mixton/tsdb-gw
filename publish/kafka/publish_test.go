package kafka

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/Shopify/sarama"
	"github.com/Shopify/sarama/mocks"
	"github.com/grafana/metrictank/cluster/partitioner"
	"github.com/raintank/schema"
	"github.com/raintank/tsdb-gw/publish/kafka/keycache"
	"github.com/raintank/tsdb-gw/util"
)

func Test_parseTopicSettings(t *testing.T) {
	tests := []struct {
		name                string
		partitionSchemesStr string
		topicsStr           string
		onlyOrgIdsStr       string
		discardPrefixesStr  string
		expected            []topicSettings
		wantErr             bool
	}{
		{
			name:                "no_topic",
			partitionSchemesStr: "",
			topicsStr:           "",
			onlyOrgIdsStr:       "",
			discardPrefixesStr:  "",
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
					onlyOrgId:       0,
					discardPrefixes: nil,
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_same_scheme",
			partitionSchemesStr: "bySeries,bySeries",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_different_scheme",
			partitionSchemesStr: "bySeries,byOrg",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_different_scheme_orgid",
			partitionSchemesStr: "bySeries,byOrg",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "1,10",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 1,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 10,
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_mismatched_scheme_count",
			partitionSchemesStr: "bySeries,byOrg,byOrg",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
			},
			wantErr: true,
		},
		{
			name:                "two_topics_mismatched_only_org_id_count",
			partitionSchemesStr: "bySeries,byOrg",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "1,10,42",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 1,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 10,
				},
			},
			wantErr: true,
		},
		{
			name:                "two_topics_with_spaces",
			partitionSchemesStr: "bySeries  ,byOrg",
			topicsStr:           "testTopic1,  testTopic2",
			onlyOrgIdsStr:       "",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_shared_scheme",
			partitionSchemesStr: "bySeries",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_shared_scheme_and_org_id",
			partitionSchemesStr: "bySeries",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "10",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 10,
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 10,
				},
			},
			wantErr: false,
		},
		{
			name:                "one_topic_mismatched_more_schemes",
			partitionSchemesStr: "bySeries,byOrg",
			topicsStr:           "testTopic",
			onlyOrgIdsStr:       "",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0,
				},
			},
			wantErr: true,
		},
		{
			name:                "one_topic_mismatched_more_discard_prefixes",
			partitionSchemesStr: "bySeries",
			topicsStr:           "testTopic",
			discardPrefixesStr:  "prefix1a-|prefix1b,prefix2",
			onlyOrgIdsStr:       "",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId:       0,
					discardPrefixes: []string{"prefix1a-|prefix1b"},
				},
			},
			wantErr: true,
		},
		{
			name:                "single_topic_discard_prefix",
			partitionSchemesStr: "bySeries",
			topicsStr:           "testTopic",
			discardPrefixesStr:  "prefix1",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId:       0,
					discardPrefixes: []string{"prefix1"},
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_discard_many_prefixes",
			partitionSchemesStr: "bySeries,bySeries",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "",
			discardPrefixesStr:  "prefix1a|prefix1b|prefix1c,prefix2a|prefix2b",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId:       0,
					discardPrefixes: []string{"prefix1a", "prefix1b", "prefix1c"},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId:       0,
					discardPrefixes: []string{"prefix2a", "prefix2b"},
				},
			},
			wantErr: false,
		},
		{
			name:                "two_topics_discard_prefix_second_only",
			partitionSchemesStr: "bySeries,bySeries",
			topicsStr:           "testTopic1,testTopic2",
			onlyOrgIdsStr:       "",
			discardPrefixesStr:  ",prefix2a|prefix2b",
			expected: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId:       0,
					discardPrefixes: []string{},
				},
				topicSettings{
					name: "testTopic2",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId:       0,
					discardPrefixes: []string{"prefix2a", "prefix2b"},
				},
			},
			wantErr: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			onlyOrgIds := util.Int64SliceFlag{}
			onlyOrgIds.Set(test.onlyOrgIdsStr)
			got, err := parseTopicSettings(test.partitionSchemesStr, test.topicsStr, onlyOrgIds, test.discardPrefixesStr)
			if (err != nil) != test.wantErr {
				t.Errorf("parseTopicSettings() error = %v, wantErr %v", err, test.wantErr)
				return
			}
			for i, topicSetting := range got {
				if topicSetting.name != test.expected[i].name {
					t.Errorf("parseTopicSettings(): incorrect topic name %s, expects %s", topicSetting.name, test.expected[i].name)
				}
				if topicSetting.onlyOrgId != test.expected[i].onlyOrgId {
					t.Errorf("parseTopicSettings(): incorrect onlyOrgId %d, expects %d", topicSetting.onlyOrgId, test.expected[i].onlyOrgId)
				}
				if topicSetting.partitioner.PartitionBy != test.expected[i].partitioner.PartitionBy {
					t.Errorf("parseTopicSettings(): incorrect partition scheme %s, expects %s", topicSetting.partitioner.PartitionBy, test.expected[i].partitioner.PartitionBy)
				}
				if topicSetting.partitioner.Partitioner == nil {
					t.Errorf("parseTopicSettings(): nil partitioner")
				}
				if !reflect.DeepEqual(topicSetting.discardPrefixes, test.expected[i].discardPrefixes) {
					t.Errorf("parseTopicSettings(): incorrect discard prefixes %v, expects %v", topicSetting.discardPrefixes, test.expected[i].discardPrefixes)
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

	dataOrgId1 := []*schema.MetricData{
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
	}
	dataOrgId10 := []*schema.MetricData{
		{
			Name:     "a.b.c10",
			OrgId:    10,
			Interval: 10,
		},
		{
			Name:     "a.b.c20",
			OrgId:    10,
			Interval: 10,
		},
	}
	dataManyOrgIds := append(dataOrgId1, dataOrgId10...)
	for _, metric := range dataManyOrgIds {
		metric.SetId()
	}

	tests := []struct {
		name         string
		topics       []topicSettings
		data         []*schema.MetricData
		expectedData map[string][]*schema.MetricData
		wantErr      bool
	}{
		{
			name:         "no_topic",
			topics:       []topicSettings{},
			data:         dataManyPoints,
			expectedData: nil,
			wantErr:      false,
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
			data: dataSinglePoint,
			expectedData: map[string][]*schema.MetricData{
				"testTopic": dataSinglePoint,
			},
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
			data: dataManyPoints,
			expectedData: map[string][]*schema.MetricData{
				"testTopic": dataManyPoints,
			},
			wantErr: false,
		},
		{
			name: "single_topic_restricted_org_id",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 10,
				},
			},
			data: dataManyOrgIds,
			expectedData: map[string][]*schema.MetricData{
				"testTopic": dataOrgId10,
			},
			wantErr: false,
		},
		{
			name: "single_topic_discard_all_prefixes",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					discardPrefixes: []string{"a.b.c"},
				},
			},
			data:         dataManyPoints,
			expectedData: nil,
			wantErr:      false,
		},
		{
			name: "single_topic_discard_one_prefix",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					discardPrefixes: []string{"a.b.c2"},
				},
			},
			data: dataManyPoints,
			expectedData: map[string][]*schema.MetricData{
				"testTopic": dataManyPoints[:len(dataManyPoints)-1],
			},
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
			data: dataSinglePoint,
			expectedData: map[string][]*schema.MetricData{
				"testTopic1": dataSinglePoint,
				"testTopic2": dataSinglePoint,
			},
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
			data: dataManyPoints,
			expectedData: map[string][]*schema.MetricData{
				"testTopic1": dataManyPoints,
				"testTopic2": dataManyPoints,
			},
			wantErr: false,
		},
		{
			name: "two_topics_many_points_discard_prefix_one_topic",
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
					discardPrefixes: []string{"a.b.c2"},
				},
			},
			data: dataManyPoints,
			expectedData: map[string][]*schema.MetricData{
				"testTopic1": dataManyPoints,
				"testTopic2": dataManyPoints[:len(dataManyPoints)-1],
			},
			wantErr: false,
		},
		{
			name: "two_topics_many_points_restricted_org_id",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 1,
				},
				topicSettings{
					name: "testTopic10",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 10,
				},
			},
			data: dataManyOrgIds,
			expectedData: map[string][]*schema.MetricData{
				"testTopic1":  dataOrgId1,
				"testTopic10": dataOrgId10,
			},
			wantErr: false,
		},
		{
			name: "two_topics_many_points_restricted_org_id_with_0",
			topics: []topicSettings{
				topicSettings{
					name: "testTopic1",
					partitioner: &partitioner.Kafka{
						PartitionBy: "bySeries",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 0, // 0 means no restriction on orgid
				},
				topicSettings{
					name: "testTopic10",
					partitioner: &partitioner.Kafka{
						PartitionBy: "byOrg",
						Partitioner: sarama.NewHashPartitioner(""),
					},
					onlyOrgId: 10,
				},
			},
			data: dataManyOrgIds,
			expectedData: map[string][]*schema.MetricData{
				"testTopic1":  dataManyOrgIds,
				"testTopic10": dataOrgId10,
			},
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

			// check that each MetricData is sent to each topic (respecting onlyOrgId) when calling Publish
			for _, metricData := range test.data {
				for _, topic := range test.topics {
					if !sliceContains(test.expectedData[topic.name], metricData) {
						// metricData is not supposed to be sent to topic
						continue
					}
					expectedMd := metricData
					mockProducer.ExpectSendMessageWithCheckerFunctionAndSucceed(func(sentData []byte) error {
						expectedDataBuf := make([]byte, 0)
						expectedData, err := expectedMd.MarshalMsg(expectedDataBuf)
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

func sliceContains(slice []*schema.MetricData, element *schema.MetricData) bool {
	for _, x := range slice {
		if x == element {
			return true
		}
	}
	return false
}
