package kafka

import (
	"errors"
	"flag"
	"hash"
	"hash/fnv"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	p "github.com/grafana/metrictank/cluster/partitioner"
	"github.com/grafana/metrictank/stats"
	"github.com/raintank/tsdb-gw/publish/kafka/keycache"
	"github.com/raintank/tsdb-gw/usage"
	"github.com/raintank/tsdb-gw/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/raintank/schema.v1"
	"gopkg.in/raintank/schema.v1/msg"
)

var (
	config   *kafka.ConfigMap
	producer *kafka.Producer
	brokers  []string
	keyCache *keycache.KeyCache
	// partitioner only needs to be initialized once since its configuration
	// won't change during runtime and a single instance can be used by many
	// threads
	kafkaPartitioner *p.Kafka

	metricsPublished = stats.NewCounter32("metrics.published")
	messagesSize     = stats.NewMeter32("metrics.message_size", false)
	publishDuration  = stats.NewLatencyHistogram15s32("metrics.publish")
	sendErrProducer  = stats.NewCounter32("metrics.send_error.producer")
	sendErrOther     = stats.NewCounter32("metrics.send_error.other")

	topic            string
	codec            string
	enabled          bool
	partitionScheme  string
	maxInFlight      int
	bufferMaxMs      int
	bufferMaxMsgs    int
	batchNumMessages int
	partitionCount   int32
	v2               bool
	v2Org            bool
	v2StaleThresh    time.Duration
	v2PruneInterval  time.Duration

	bufferPool      = util.NewBufferPool()
	bufferPool33    = util.NewBufferPool33()
	partitionerPool sync.Pool
)

type mtPublisher struct{}

type Partitioner interface {
	partition(schema.PartitionedMetric) (int32, []byte, error)
}

func NewPartitioner() Partitioner {
	return &partitionerFnv1a{
		hasher: fnv.New32a(),
	}
}

type partitionerFnv1a struct {
	hasher hash.Hash32
}

func (p *partitionerFnv1a) partition(m schema.PartitionedMetric) (int32, []byte, error) {
	key, err := kafkaPartitioner.GetPartitionKey(m, nil)
	if err != nil {
		return -1, nil, err
	}

	p.hasher.Reset()
	_, err = p.hasher.Write(key)
	if err != nil {
		return -1, nil, err
	}

	partition := int32(p.hasher.Sum32()) % partitionCount
	if partition < 0 {
		partition = -partition
	}

	return partition, key, nil
}

func init() {
	flag.StringVar(&topic, "metrics-topic", "mdm", "topic for metrics")
	flag.StringVar(&codec, "metrics-kafka-comp", "snappy", "compression: none|gzip|snappy")
	flag.BoolVar(&enabled, "metrics-publish", false, "enable metric publishing")
	flag.StringVar(&partitionScheme, "metrics-partition-scheme", "bySeries", "method used for paritioning metrics. (byOrg|bySeries)")
	flag.IntVar(&maxInFlight, "metrics-max-in-flight", 1000000, "The maximum number of requests in flight per broker connection")
	flag.IntVar(&bufferMaxMsgs, "metrics-buffer-max-msgs", 100000, "Maximum number of messages allowed on the producer queue. Publishing attempts will be rejected once this limit is reached.")
	flag.IntVar(&bufferMaxMs, "metrics-buffer-max-ms", 100, "Delay in milliseconds to wait for messages in the producer queue to accumulate before constructing message batches (MessageSets) to transmit to brokers")
	flag.IntVar(&batchNumMessages, "batch-num-messages", 10000, "Maximum number of messages batched in one MessageSet")

	flag.BoolVar(&v2, "v2", true, "enable optimized MetricPoint payload")
	flag.BoolVar(&v2Org, "v2-org", true, "encode org-id in messages")
	flag.DurationVar(&v2StaleThresh, "v2-stale-thresh", 6*time.Hour, "expire keys (and resend MetricData if seen again) if not seen for this much time")
	flag.DurationVar(&v2PruneInterval, "v2-prune-interval", time.Hour, "check interval for expiring keys")
}

func New(broker string) *mtPublisher {
	if !enabled {
		return nil
	}
	var err error

	config := kafka.ConfigMap{}
	config.SetKey("request.required.acks", "all")
	config.SetKey("message.send.max.retries", "10")
	config.SetKey("bootstrap.servers", broker)
	config.SetKey("compression.codec", codec)
	config.SetKey("max.in.flight", maxInFlight)
	config.SetKey("queue.buffering.max.ms", bufferMaxMs)
	config.SetKey("batch.num.messages", batchNumMessages)
	config.SetKey("queue.buffering.max.messages", bufferMaxMsgs)

	producer, err = kafka.NewProducer(&config)
	if err != nil {
		log.Fatalf("failed to initialize kafka producer. %s", err)
	}

	meta, err := producer.GetMetadata(&topic, false, 30000)
	if err != nil {
		log.Fatalf("failed to initialize kafka partitioner. %s", err)
	}

	var t kafka.TopicMetadata
	var ok bool
	if t, ok = meta.Topics[topic]; !ok {
		log.Fatalf("failed to get metadata about topic %s", topic)
	}

	partitionCount = int32(len(t.Partitions))
	kafkaPartitioner, err = p.NewKafka(partitionScheme)
	if err != nil {
		log.Fatalf("failed to initialize partitioner. %s", err)
	}

	if v2 {
		keyCache = keycache.NewKeyCache(v2StaleThresh, v2PruneInterval)
	}

	partitionerPool = sync.Pool{
		New: func() interface{} { return NewPartitioner() },
	}
	return &mtPublisher{}
}

func (m *mtPublisher) Publish(metrics []*schema.MetricData) error {
	if producer == nil {
		log.Debugf("dropping %d metrics as publishing is disabled", len(metrics))
		return nil
	}
	if len(metrics) == 0 {
		return nil
	}
	var err error

	payload := make([]*kafka.Message, len(metrics))
	pre := time.Now()
	deliveryChan := make(chan kafka.Event, len(metrics))
	partitioner := partitionerPool.Get().(Partitioner)

	for i, metric := range metrics {
		var data []byte
		if v2 {
			var mkey schema.MKey
			mkey, err = schema.MKeyFromString(metric.Id)
			if err != nil {
				return err
			}
			ok := keyCache.Touch(mkey, pre)
			// we've seen this key recently. we can use the optimized format
			if ok {
				data = bufferPool33.Get()
				mp := schema.MetricPoint{
					MKey:  mkey,
					Value: metric.Value,
					Time:  uint32(metric.Time),
				}
				if v2Org {
					data[:1][0] = byte(msg.FormatMetricPoint)
					_, err = mp.Marshal32(data[1:])
					data = data[:33]
				} else {
					data[:1][0] = byte(msg.FormatMetricPointWithoutOrg)
					_, err = mp.MarshalWithoutOrg28(data[1:])
					data = data[:29]
				}
			} else {
				data = bufferPool.Get()
				data, err = metric.MarshalMsg(data)
				if err != nil {
					return err
				}
			}
		} else {
			data = bufferPool.Get()
			data, err = metric.MarshalMsg(data)
			if err != nil {
				return err
			}
		}

		part, key, err := partitioner.partition(metric)
		if err != nil {
			return err
		}

		payload[i] = &kafka.Message{
			TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: part},
			Value:          data,
			Key:            key,
		}

		messagesSize.Value(len(data))

		err = producer.Produce(payload[i], deliveryChan)
		if err != nil {
			return err
		}
	}

	partitionerPool.Put(partitioner)

	// return buffers to the bufferPool
	defer func() {
		var buf []byte
		for _, msg := range payload {
			buf = msg.Value
			if cap(buf) == 33 {
				bufferPool33.Put(buf)
			} else {
				bufferPool.Put(buf)
			}
		}
	}()

	msgCount := 0
	var errCount int
	var firstErr error
	for e := range deliveryChan {
		msgCount++

		err = nil
		m, ok := e.(*kafka.Message)
		if !ok || e == nil {
			log.Errorf("unexpected delivery report of type %T: %v", e, e)
			err = errors.New("Invalid acknowledgement")
		} else if m.TopicPartition.Error != nil {
			err = m.TopicPartition.Error
		}

		if err != nil {
			errCount++
			sendErrOther.Inc()
			if firstErr == nil {
				firstErr = err
			}
		}

		if msgCount >= len(metrics) {
			close(deliveryChan)
		}
	}

	if firstErr != nil {
		log.Errorf("Got %d errors when sending %d messages, the first was: %s", errCount, len(metrics), firstErr)
		return firstErr
	}

	publishDuration.Value(time.Since(pre))
	metricsPublished.Add(len(metrics))
	log.Debugf("published %d metrics", len(metrics))
	for _, metric := range metrics {
		usage.LogDataPoint(metric.Id)
	}
	return nil
}

func (*mtPublisher) Type() string {
	return "Metrictank"
}