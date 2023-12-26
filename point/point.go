package point

import (
	"math/rand"
	"sync/atomic"
	"time"

	"github.com/chengshiwen/influx-stress/lineprotocol"
)

// The point struct implements the lineprotocol.Point interface.
type Point struct {
	seriesKey []byte

	// Note here that Ints and Floats are exported so they can be modified outside
	// of the point struct
	Ints   []*lineprotocol.Int
	Floats []*lineprotocol.Float

	// The fields slice should contain exactly Ints and Floats. Having this
	// slice allows us to avoid iterating through Ints and Floats in the Fields
	// function.
	fields []lineprotocol.Field

	time *lineprotocol.Timestamp
}

// New returns a new point without setting the time field.
func New(sk []byte, ints, floats []string, p lineprotocol.Precision) *Point {
	fields := []lineprotocol.Field{}
	e := &Point{
		seriesKey: sk,
		time:      lineprotocol.NewTimestamp(p),
		fields:    fields,
	}

	for _, i := range ints {
		n := &lineprotocol.Int{Key: []byte(i)}
		e.Ints = append(e.Ints, n)
		e.fields = append(e.fields, n)
	}

	for _, f := range floats {
		n := &lineprotocol.Float{Value: 0.2, Key: []byte(f)}
		e.Floats = append(e.Floats, n)
		e.fields = append(e.fields, n)
	}

	return e
}

// Series returns the series key for a point.
func (p *Point) Series() []byte {
	return p.seriesKey
}

// Fields returns the fields for a a point.
func (p *Point) Fields() []lineprotocol.Field {
	return p.fields
}

// Time returns the timestamps for a point.
func (p *Point) Time() *lineprotocol.Timestamp {
	return p.time
}

// SetTime set the t to be the timestamp for a point.
func (p *Point) SetTime(t time.Time) {
	p.time.SetTime(&t)
}

// Update increments the value of all of the Int and Float
// fields randomly.
func (p *Point) Update() {
	for _, i := range p.Ints {
		atomic.StoreInt64(&i.Value, rand.Int63n(100))
	}

	for _, f := range p.Floats {
		f.Value = rand.ExpFloat64() * 100
	}
}

// NewPoints returns a slice of Points of length seriesN shaped like the given seriesKey.
func NewPoints(seriesKey, fields string, seriesN int, pc lineprotocol.Precision) []lineprotocol.Point {
	pts := []lineprotocol.Point{}
	series := generateSeriesKeys(seriesKey, seriesN)
	ints, floats := generateFieldSet(fields)
	for _, sk := range series {
		p := New(sk, ints, floats, pc)
		pts = append(pts, p)
	}

	return pts
}
