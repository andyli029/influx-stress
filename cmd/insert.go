package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chengshiwen/influx-stress/lineprotocol"
	"github.com/chengshiwen/influx-stress/point"
	"github.com/chengshiwen/influx-stress/stress"
	"github.com/chengshiwen/influx-stress/write"
	"github.com/spf13/cobra"
)

var (
	statsHost, statsDB                   string
	host, db, rp, precision, consistency string
	username, password                   string
	createCommand, dump                  string
	seriesN, gzip                        int
	batchSize, pointsN, pps              uint64
	runtime                              time.Duration
	tick                                 time.Duration
	fast, quiet                          bool
	strict                               bool
	recordStats                          bool
	tlsSkipVerify                        bool
	readTimeout, writeTimeout            time.Duration
)

const (
	defaultSeriesKey string = "ctr,some=ajIpepzFPgntTuLxMYCkbdCJYwfxXZ"
	defaultSeriesKey1 string = "ctr,some="
	defaultFieldStr  string = "n=0"
)

var insertCmd = &cobra.Command{
	Use:   "insert SERIES FIELDS",
	Short: "Insert data into InfluxDB", // better description
	Long:  "",
	Run:   insertRun,
}

func RandStrStr(length int) string {
	str := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	bytes := []byte(str)
	result := []byte{}
	rand.Seed(time.Now().UnixNano()+ int64(rand.Intn(100)))
	for i := 0; i < length; i++ {
		result = append(result, bytes[rand.Intn(len(bytes))])
	}
	return string(result)
}

func insertRun(cmd *cobra.Command, args []string) {
	seriesKey := defaultSeriesKey1 + RandStrStr(20)
	fieldStr := defaultFieldStr
	if len(args) >= 1 {
		seriesKey = args[0]
		commaIndex := strings.Index(seriesKey, ",")
		equalIndex := strings.Index(seriesKey, "=")
		if !(0 < commaIndex && commaIndex < equalIndex && equalIndex < len(seriesKey)-1) {
			fmt.Fprintf(os.Stderr, "First positional argument must be a series key such as \"%s\", got: %s\n", defaultSeriesKey, seriesKey)
			cmd.Usage()
			os.Exit(1)
			return
		}
	}
	if len(args) == 2 {
		fieldStr = args[1]
	}

	concurrency := pps / batchSize
	// PPS takes precedence over batchSize.
	// Adjust accordingly.
	if pps < batchSize {
		batchSize = pps
		concurrency = 1
	}
	if uint64(seriesN) < batchSize {
		// otherwise, the tool will generate points with identical timestamp
		fmt.Fprintf(os.Stderr, "series number(%v) should not be smaller than batch size(%v)\n", seriesN, batchSize)
		os.Exit(1)
		return
	}
	if !quiet {
		fmt.Printf("Using point template: %s %s <timestamp>\n", seriesKey, fieldStr)
		fmt.Printf("Using batch size of %d line(s)\n", batchSize)
		fmt.Printf("Spreading writes across %d series\n", seriesN)
		if fast {
			fmt.Println("Output is unthrottled")
		} else {
			fmt.Printf("Throttling output to ~%d points/sec\n", pps)
		}
		fmt.Printf("Using %d concurrent writer(s)\n", concurrency)

		fmt.Printf("Running until ~%d points sent or until ~%v has elapsed\n", pointsN, runtime)
	}

	c := client()

	pts := point.NewPoints(seriesKey, fieldStr, seriesN, lineprotocol.Nanosecond)

	startSplit := 0
	inc := int(seriesN) / int(concurrency)
	endSplit := inc

	sink := newMultiSink(int(concurrency))
	sink.AddSink(newErrorSink(int(concurrency)))

	if recordStats {
		sink.AddSink(newInfluxDBSink(int(concurrency), statsHost, statsDB))
	}

	sink.Open()

	var wg sync.WaitGroup
	wg.Add(int(concurrency))

	var totalWritten uint64

	start := time.Now()
	for i := uint64(0); i < concurrency; i++ {

		go func(startSplit, endSplit int) {
			tick := time.Tick(tick)

			if fast {
				tick = time.Tick(time.Nanosecond)
			}

			cfg := stress.WriteConfig{
				BatchSize: batchSize,
				MaxPoints: pointsN / concurrency, // divide by concurrency
				GzipLevel: gzip,
				Deadline:  time.Now().Add(runtime),
				Tick:      tick,
				Results:   sink.Chan(),
			}

			// Ignore duration from a single call to Write.
			pointsWritten, _ := stress.Write(pts[startSplit:endSplit], c, cfg)
			atomic.AddUint64(&totalWritten, pointsWritten)

			wg.Done()
		}(startSplit, endSplit)

		startSplit = endSplit
		endSplit += inc
	}

	wg.Wait()
	totalTime := time.Since(start)
	if err := c.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "Error closing client: %v\n", err.Error())
	}

	defer sink.Close()
	throughput := int(float64(totalWritten) / totalTime.Seconds())
	if quiet {
		fmt.Println(throughput)
	} else {
		fmt.Println("Write Throughput:", throughput)
		fmt.Println("Points Written:", totalWritten)
	}
}

func init() {
	RootCmd.AddCommand(insertCmd)
	insertCmd.Flags().StringVarP(&statsHost, "stats-host", "", "http://localhost:8086", "Address of InfluxDB instance where runtime statistics will be recorded")
	insertCmd.Flags().StringVarP(&statsDB, "stats-db", "", "stress_stats", "Database that statistics will be written to")
	insertCmd.Flags().BoolVarP(&recordStats, "stats", "", false, "Record runtime statistics")
	insertCmd.Flags().StringVarP(&host, "host", "", "http://localhost:8086", "Address of InfluxDB instance")
	insertCmd.Flags().StringVarP(&username, "user", "", "", "User to write data as")
	insertCmd.Flags().StringVarP(&password, "pass", "", "", "Password for user")
	insertCmd.Flags().StringVarP(&db, "db", "", "stress", "Database that will be written to")
	insertCmd.Flags().StringVarP(&rp, "rp", "", "", "Retention Policy that will be written to")
	insertCmd.Flags().StringVarP(&precision, "precision", "p", "n", "Resolution of data being written")
	insertCmd.Flags().StringVarP(&consistency, "consistency", "c", "one", "Write consistency (only applicable to clusters)")
	insertCmd.Flags().IntVarP(&seriesN, "series", "s", 100000, "number of series that will be written")
	insertCmd.Flags().Uint64VarP(&pointsN, "points", "n", math.MaxUint64, "number of points that will be written")
	insertCmd.Flags().Uint64VarP(&batchSize, "batch-size", "b", 10000, "number of points in a batch")
	insertCmd.Flags().Uint64VarP(&pps, "pps", "", 200000, "Points Per Second")
	insertCmd.Flags().DurationVarP(&runtime, "runtime", "r", time.Duration(math.MaxInt64), "Total time that the test will run")
	insertCmd.Flags().DurationVarP(&tick, "tick", "", time.Second, "Amount of time between request")
	insertCmd.Flags().BoolVarP(&fast, "fast", "f", false, "Run as fast as possible")
	insertCmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Only print the write throughput")
	insertCmd.Flags().StringVar(&createCommand, "create", "", "Use a custom create database command")
	insertCmd.Flags().IntVar(&gzip, "gzip", 0, "If non-zero, gzip write bodies with given compression level. 1=best speed, 9=best compression, -1=gzip default.")
	insertCmd.Flags().StringVar(&dump, "dump", "", "Dump to given file instead of writing over HTTP")
	insertCmd.Flags().BoolVarP(&strict, "strict", "", false, "Strict mode will exit as soon as an error or unexpected status is encountered")
	insertCmd.Flags().BoolVarP(&tlsSkipVerify, "tls-skip-verify", "", false, "Skip verify in for TLS")
	insertCmd.Flags().DurationVarP(&readTimeout, "read-timeout", "", 0, "read timeout")
	insertCmd.Flags().DurationVarP(&writeTimeout, "write-timeout", "", 0, "write timeout")
}

func client() write.Client {
	cfg := write.ClientConfig{
		BaseURL:         host,
		Database:        db,
		RetentionPolicy: rp,
		User:            username,
		Pass:            password,
		Precision:       precision,
		Consistency:     consistency,
		TLSSkipVerify:   tlsSkipVerify,
		ReadTimeout:     readTimeout,
		WriteTimeout:    writeTimeout,
		Gzip:            gzip != 0,
	}

	if dump != "" {
		c, err := write.NewFileClient(dump, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error opening file:", err)
			os.Exit(1)
			return c
		}

		return c
	}
	return write.NewClient(cfg)
}

type Sink interface {
	Chan() chan stress.WriteResult
	Open()
	Close()
}

type errorSink struct {
	Ch chan stress.WriteResult

	wg sync.WaitGroup
}

func newErrorSink(nWriters int) *errorSink {
	s := &errorSink{
		Ch: make(chan stress.WriteResult, 8*nWriters),
	}

	s.wg.Add(1)
	go s.checkErrors()

	return s
}

func (s *errorSink) Open() {
	s.wg.Add(1)
	go s.checkErrors()
}

func (s *errorSink) Close() {
	close(s.Ch)
	s.wg.Wait()
}

func (s *errorSink) checkErrors() {
	defer s.wg.Done()

	const timeFormat = "[2006-01-02 15:04:05]"
	for r := range s.Ch {
		if r.Err != nil {
			fmt.Fprintln(os.Stderr, time.Now().Format(timeFormat), "Error sending write:", r.Err.Error())
			continue
		}

		if r.StatusCode != 204 {
			fmt.Fprintln(os.Stderr, time.Now().Format(timeFormat), "Unexpected write: status", r.StatusCode, ", body:", r.Body)
		}

		// If we're running in strict mode then we give up at the first error.
		if strict && (r.Err != nil || r.StatusCode != 204) {
			os.Exit(1)
		}
	}
}

func (s *errorSink) Chan() chan stress.WriteResult {
	return s.Ch
}

type multiSink struct {
	Ch chan stress.WriteResult

	sinks []Sink

	open bool
}

func newMultiSink(nWriters int) *multiSink {
	return &multiSink{
		Ch: make(chan stress.WriteResult, 8*nWriters),
	}
}

func (s *multiSink) Chan() chan stress.WriteResult {
	return s.Ch
}

func (s *multiSink) Open() {
	s.open = true
	for _, sink := range s.sinks {
		sink.Open()
	}

	go s.run()
}

func (s *multiSink) run() {
	const timeFormat = "[2006-01-02 15:04:05]"
	defer func() {
		// recover from panic caused by writing to a closed channel
		if r := recover(); r != nil {
			// fmt.Fprintln(os.Stderr, time.Now().Format(timeFormat), r)
			return
		}
	}()
	for r := range s.Ch {
		for _, sink := range s.sinks {
			select {
			case sink.Chan() <- r:
			default:
				fmt.Fprintln(os.Stderr, time.Now().Format(timeFormat), "Failed to send to sin")
			}
		}
	}
}

func (s *multiSink) Close() {
	s.open = false
	close(s.Ch)
	for _, sink := range s.sinks {
		sink.Close()
	}
}

func (s *multiSink) AddSink(sink Sink) error {
	if s.open {
		return errors.New("Cannot add sink to open multiSink")
	}

	s.sinks = append(s.sinks, sink)

	return nil
}

type influxDBSink struct {
	Ch     chan stress.WriteResult
	client write.Client
	buf    *bytes.Buffer
	ticker *time.Ticker
}

func newInfluxDBSink(nWriters int, url, db string) *influxDBSink {
	cfg := write.ClientConfig{
		BaseURL:         url,
		Database:        db,
		RetentionPolicy: "autogen",
		Precision:       "ns",
		Consistency:     "any",
		Gzip:            false,
	}

	return &influxDBSink{
		Ch:     make(chan stress.WriteResult, 8*nWriters),
		client: write.NewClient(cfg),
		buf:    bytes.NewBuffer(nil),
	}
}

func (s *influxDBSink) Chan() chan stress.WriteResult {
	return s.Ch
}

func (s *influxDBSink) Open() {
	s.ticker = time.NewTicker(time.Second)
	err := s.client.Create("")
	if err != nil {
		panic(err)
	}

	go s.run()
}

func (s *influxDBSink) Close() {
}

func (s *influxDBSink) run() {
	for {
		select {
		case <-s.ticker.C:
			// Write batch
			s.client.Send(s.buf.Bytes())
			s.buf.Reset()
		case result := <-s.Ch:
			// Add to batch
			if result.Err != nil {
				continue
			}
			s.buf.WriteString(fmt.Sprintf("req,status=%v latNs=%v %v\n", result.StatusCode, result.LatNs, result.Timestamp))
		}
	}
}
