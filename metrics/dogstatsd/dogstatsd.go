// Package dogstatsd provides a DogStatsD backend for package metrics. It's very
// similar to StatsD, but supports arbitrary tags per-metric, which map to Go
// kit's label values. So, while label values are no-ops in StatsD, they are
// supported here. For more details, see the documentation at
// http://docs.datadoghq.com/guides/dogstatsd/.
//
// This package batches observations and emits them on some schedule to the
// remote server. This is useful even if you connect to your DogStatsD server
// over UDP. Emitting one network packet per observation can quickly overwhelm
// even the fastest internal network.
package dogstatsd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/guherbozdogan/kit/log"
	"github.com/guherbozdogan/kit/metrics"
	"github.com/guherbozdogan/kit/metrics/internal/lv"
	"github.com/guherbozdogan/kit/metrics/internal/ratemap"
	"github.com/guherbozdogan/kit/util/conn"
)

// Dogstatsd receives metrics observations and forwards them to a DogStatsD
// server. Create a Dogstatsd object, use it to create metrics, and pass those
// metrics as dependencies to the components that will use them.
//
// All metrics are buffered until WriteTo is called. Counters and gauges are
// aggregated into a single observation per timeseries per write. Timings and
// histograms are buffered but not aggregated.
//
// To regularly report metrics to an io.Writer, use the WriteLoop helper method.
// To send to a DogStatsD server, use the SendLoop helper method.
type Dogstatsd struct {
	prefix     string
	rates      *ratemap.RateMap
	counters   *lv.Space
	gauges     *lv.Space
	timings    *lv.Space
	histograms *lv.Space
	logger     log.Logger
}

// New returns a Dogstatsd object that may be used to create metrics. Prefix is
// applied to all created metrics. Callers must ensure that regular calls to
// WriteTo are performed, either manually or with one of the helper methods.
func New(prefix string, logger log.Logger) *Dogstatsd {
	return &Dogstatsd{
		prefix:     prefix,
		rates:      ratemap.New(),
		counters:   lv.NewSpace(),
		gauges:     lv.NewSpace(),
		timings:    lv.NewSpace(),
		histograms: lv.NewSpace(),
		logger:     logger,
	}
}

// NewCounter returns a counter, sending observations to this Dogstatsd object.
func (d *Dogstatsd) NewCounter(name string, sampleRate float64) *Counter {
	d.rates.Set(d.prefix+name, sampleRate)
	return &Counter{
		name: d.prefix + name,
		obs:  d.counters.Observe,
	}
}

// NewGauge returns a gauge, sending observations to this Dogstatsd object.
func (d *Dogstatsd) NewGauge(name string) *Gauge {
	return &Gauge{
		name: d.prefix + name,
		obs:  d.gauges.Observe,
		add:  d.gauges.Add,
	}
}

// NewTiming returns a histogram whose observations are interpreted as
// millisecond durations, and are forwarded to this Dogstatsd object.
func (d *Dogstatsd) NewTiming(name string, sampleRate float64) *Timing {
	d.rates.Set(d.prefix+name, sampleRate)
	return &Timing{
		name: d.prefix + name,
		obs:  d.timings.Observe,
	}
}

// NewHistogram returns a histogram whose observations are of an unspecified
// unit, and are forwarded to this Dogstatsd object.
func (d *Dogstatsd) NewHistogram(name string, sampleRate float64) *Histogram {
	d.rates.Set(d.prefix+name, sampleRate)
	return &Histogram{
		name: d.prefix + name,
		obs:  d.histograms.Observe,
	}
}

// WriteLoop is a helper method that invokes WriteTo to the passed writer every
// time the passed channel fires. This method blocks until the channel is
// closed, so clients probably want to run it in its own goroutine. For typical
// usage, create a time.Ticker and pass its C channel to this method.
func (d *Dogstatsd) WriteLoop(c <-chan time.Time, w io.Writer) {
	for range c {
		if _, err := d.WriteTo(w); err != nil {
			d.logger.Log("during", "WriteTo", "err", err)
		}
	}
}

// SendLoop is a helper method that wraps WriteLoop, passing a managed
// connection to the network and address. Like WriteLoop, this method blocks
// until the channel is closed, so clients probably want to start it in its own
// goroutine. For typical usage, create a time.Ticker and pass its C channel to
// this method.
func (d *Dogstatsd) SendLoop(c <-chan time.Time, network, address string) {
	d.WriteLoop(c, conn.NewDefaultManager(network, address, d.logger))
}

// WriteTo flushes the buffered content of the metrics to the writer, in
// DogStatsD format. WriteTo abides best-effort semantics, so observations are
// lost if there is a problem with the write. Clients should be sure to call
// WriteTo regularly, ideally through the WriteLoop or SendLoop helper methods.
func (d *Dogstatsd) WriteTo(w io.Writer) (count int64, err error) {
	var n int

	d.counters.Reset().Walk(func(name string, lvs lv.LabelValues, values []float64) bool {
		n, err = fmt.Fprintf(w, "%s:%f|c%s%s\n", name, sum(values), sampling(d.rates.Get(name)), tagValues(lvs))
		if err != nil {
			return false
		}
		count += int64(n)
		return true
	})
	if err != nil {
		return count, err
	}

	d.gauges.Reset().Walk(func(name string, lvs lv.LabelValues, values []float64) bool {
		n, err = fmt.Fprintf(w, "%s:%f|g%s\n", name, last(values), tagValues(lvs))
		if err != nil {
			return false
		}
		count += int64(n)
		return true
	})
	if err != nil {
		return count, err
	}

	d.timings.Reset().Walk(func(name string, lvs lv.LabelValues, values []float64) bool {
		sampleRate := d.rates.Get(name)
		for _, value := range values {
			n, err = fmt.Fprintf(w, "%s:%f|ms%s%s\n", name, value, sampling(sampleRate), tagValues(lvs))
			if err != nil {
				return false
			}
			count += int64(n)
		}
		return true
	})
	if err != nil {
		return count, err
	}

	d.histograms.Reset().Walk(func(name string, lvs lv.LabelValues, values []float64) bool {
		sampleRate := d.rates.Get(name)
		for _, value := range values {
			n, err = fmt.Fprintf(w, "%s:%f|h%s%s\n", name, value, sampling(sampleRate), tagValues(lvs))
			if err != nil {
				return false
			}
			count += int64(n)
		}
		return true
	})
	if err != nil {
		return count, err
	}

	return count, err
}

func sum(a []float64) float64 {
	var v float64
	for _, f := range a {
		v += f
	}
	return v
}

func last(a []float64) float64 {
	return a[len(a)-1]
}

func sampling(r float64) string {
	var sv string
	if r < 1.0 {
		sv = fmt.Sprintf("|@%f", r)
	}
	return sv
}

func tagValues(labelValues []string) string {
	if len(labelValues) == 0 {
		return ""
	}
	if len(labelValues)%2 != 0 {
		panic("tagValues received a labelValues with an odd number of strings")
	}
	pairs := make([]string, 0, len(labelValues)/2)
	for i := 0; i < len(labelValues); i += 2 {
		pairs = append(pairs, labelValues[i]+":"+labelValues[i+1])
	}
	return "|#" + strings.Join(pairs, ",")
}

type observeFunc func(name string, lvs lv.LabelValues, value float64)

// Counter is a DogStatsD counter. Observations are forwarded to a Dogstatsd
// object, and aggregated (summed) per timeseries.
type Counter struct {
	name string
	lvs  lv.LabelValues
	obs  observeFunc
}

// With implements metrics.Counter.
func (c *Counter) With(labelValues ...string) metrics.Counter {
	return &Counter{
		name: c.name,
		lvs:  c.lvs.With(labelValues...),
		obs:  c.obs,
	}
}

// Add implements metrics.Counter.
func (c *Counter) Add(delta float64) {
	c.obs(c.name, c.lvs, delta)
}

// Gauge is a DogStatsD gauge. Observations are forwarded to a Dogstatsd
// object, and aggregated (the last observation selected) per timeseries.
type Gauge struct {
	name string
	lvs  lv.LabelValues
	obs  observeFunc
	add  observeFunc
}

// With implements metrics.Gauge.
func (g *Gauge) With(labelValues ...string) metrics.Gauge {
	return &Gauge{
		name: g.name,
		lvs:  g.lvs.With(labelValues...),
		obs:  g.obs,
		add:  g.add,
	}
}

// Set implements metrics.Gauge.
func (g *Gauge) Set(value float64) {
	g.obs(g.name, g.lvs, value)
}

// Add implements metrics.Gauge.
func (g *Gauge) Add(delta float64) {
	g.add(g.name, g.lvs, delta)
}

// Timing is a DogStatsD timing, or metrics.Histogram. Observations are
// forwarded to a Dogstatsd object, and collected (but not aggregated) per
// timeseries.
type Timing struct {
	name string
	lvs  lv.LabelValues
	obs  observeFunc
}

// With implements metrics.Timing.
func (t *Timing) With(labelValues ...string) metrics.Histogram {
	return &Timing{
		name: t.name,
		lvs:  t.lvs.With(labelValues...),
		obs:  t.obs,
	}
}

// Observe implements metrics.Histogram. Value is interpreted as milliseconds.
func (t *Timing) Observe(value float64) {
	t.obs(t.name, t.lvs, value)
}

// Histogram is a DogStatsD histrogram. Observations are forwarded to a
// Dogstatsd object, and collected (but not aggregated) per timeseries.
type Histogram struct {
	name string
	lvs  lv.LabelValues
	obs  observeFunc
}

// With implements metrics.Histogram.
func (h *Histogram) With(labelValues ...string) metrics.Histogram {
	return &Histogram{
		name: h.name,
		lvs:  h.lvs.With(labelValues...),
		obs:  h.obs,
	}
}

// Observe implements metrics.Histogram.
func (h *Histogram) Observe(value float64) {
	h.obs(h.name, h.lvs, value)
}
