// Copyright (c) 2018 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package prometheus

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
)

// Configuration is a configuration for a Prometheus reporter.
type Configuration struct {
	// HandlerPath if specified will be used instead of using the default
	// HTTP handler path "/metrics".
	HandlerPath string `yaml:"handlerPath"`

	// ListenAddress if specified will be used instead of just registering the
	// handler on the default HTTP serve mux without listening.
	ListenAddress string `yaml:"listenAddress"`

	// DynamicListenAddress if specified will be used instead of just registering the
	// handler on the default HTTP serve mux without listening.
	// Note: if DynamicListenAddress is specified, ListenAddress is ignored.
	DynamicListenAddress *ListenAddressConfiguration `yaml:"dynamicListenAddress"`

	// TimerType is the default Prometheus type to use for Tally timers.
	TimerType string `yaml:"timerType"`

	// DefaultHistogramBuckets if specified will set the default histogram
	// buckets to be used by the reporter.
	DefaultHistogramBuckets []HistogramObjective `yaml:"defaultHistogramBuckets"`

	// DefaultSummaryObjectives if specified will set the default summary
	// objectives to be used by the reporter.
	DefaultSummaryObjectives []SummaryObjective `yaml:"defaultSummaryObjectives"`

	// OnError specifies what to do when an error either with listening
	// on the specified listen address or registering a metric with the
	// Prometheus. By default the registerer will panic.
	OnError string `yaml:"onError"`
}

// HistogramObjective is a Prometheus histogram bucket.
// See: https://godoc.org/github.com/prometheus/client_golang/prometheus#HistogramOpts
type HistogramObjective struct {
	Upper float64 `yaml:"upper"`
}

// SummaryObjective is a Prometheus summary objective.
// See: https://godoc.org/github.com/prometheus/client_golang/prometheus#SummaryOpts
type SummaryObjective struct {
	Percentile   float64 `yaml:"percentile"`
	AllowedError float64 `yaml:"allowedError"`
}

// ConfigurationOptions allows some error callbacks to be registered.
type ConfigurationOptions struct {
	OnError func(e error)
}

// NewReporter creates a new M3 reporter from this configuration.
func (c Configuration) NewReporter(
	configOpts ConfigurationOptions,
) (Reporter, error) {
	if configOpts.OnError == nil {
		switch c.OnError {
		case "stderr":
			configOpts.OnError = func(err error) {
				fmt.Fprintf(os.Stderr, "tally prometheus reporter error: %v\n", err)
			}
		case "log":
			configOpts.OnError = func(err error) {
				log.Printf("tally prometheus reporter error: %v\n", err)
			}
		case "none":
			configOpts.OnError = func(err error) {}
		default:
			configOpts.OnError = func(err error) {
				panic(err)
			}
		}
	}

	var opts Options
	opts.OnRegisterError = configOpts.OnError

	switch c.TimerType {
	case "summary":
		opts.DefaultTimerType = SummaryTimerType
	case "histogram":
		opts.DefaultTimerType = HistogramTimerType
	}

	if len(c.DefaultHistogramBuckets) > 0 {
		var values []float64
		for _, value := range c.DefaultHistogramBuckets {
			values = append(values, value.Upper)
		}
		opts.DefaultHistogramBuckets = values
	}

	if len(c.DefaultSummaryObjectives) > 0 {
		values := make(map[float64]float64)
		for _, value := range c.DefaultSummaryObjectives {
			values[value.Percentile] = value.AllowedError
		}
		opts.DefaultSummaryObjectives = values
	}

	reporter := NewReporter(opts)

	path := "/metrics"
	if handlerPath := strings.TrimSpace(c.HandlerPath); handlerPath != "" {
		path = handlerPath
	}

	addr, resolved, err := c.resolveListenAddress()
	if err != nil {
		return nil, err
	}

	if !resolved {
		http.Handle(path, reporter.HTTPHandler())
	} else {
		mux := http.NewServeMux()
		mux.Handle(path, reporter.HTTPHandler())
		go func() {
			if err := http.ListenAndServe(addr, mux); err != nil {
				configOpts.OnError(err)
			}
		}()
	}

	return reporter, nil
}

func (c Configuration) resolveListenAddress() (addr string, resolved bool, err error) {
	// first try DynamicListenAddress
	if c.DynamicListenAddress != nil {
		addr, err := c.DynamicListenAddress.Resolve()
		if err != nil {
			return "", false, err
		}
		return addr, true, nil
	}

	// next, try ListenAddress
	addr = strings.TrimSpace(c.ListenAddress)
	if addr == "" {
		return "", false, nil
	}

	return addr, true, nil
}