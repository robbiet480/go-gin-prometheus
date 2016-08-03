package ginprometheus

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

var defaultMetricPath = "/metrics"

type Prometheus struct {
	reqCnt               *prometheus.CounterVec
	reqDur, reqSz, resSz prometheus.Summary

	MetricsPath string
}

func NewPrometheus(subsystem string) *Prometheus {
	p := &Prometheus{
		MetricsPath: defaultMetricPath,
	}

	p.registerMetrics(subsystem)

	return p
}

func Middleware(subsystem string) gin.HandlerFunc {
	return NewPrometheus(subsystem).handlerFunc()
}

func (p *Prometheus) registerMetrics(subsystem string) {
	p.reqCnt = prometheus.MustRegisterOrGet(prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: subsystem,
			Name:      "requests_total",
			Help:      "How many HTTP requests processed, partitioned by status code and HTTP method.",
		},
		[]string{"code", "method", "handler"},
	)).(*prometheus.CounterVec)

	p.reqDur = prometheus.MustRegisterOrGet(prometheus.NewSummary(
		prometheus.SummaryOpts{
			Subsystem: subsystem,
			Name:      "request_duration_seconds",
			Help:      "The HTTP request latencies in seconds.",
		},
	)).(prometheus.Summary)

	p.reqSz = prometheus.MustRegisterOrGet(prometheus.NewSummary(
		prometheus.SummaryOpts{
			Subsystem: subsystem,
			Name:      "request_size_bytes",
			Help:      "The HTTP request sizes in bytes.",
		},
	)).(prometheus.Summary)

	p.resSz = prometheus.MustRegisterOrGet(prometheus.NewSummary(
		prometheus.SummaryOpts{
			Subsystem: subsystem,
			Name:      "response_size_bytes",
			Help:      "The HTTP response sizes in bytes.",
		},
	)).(prometheus.Summary)
}

func (p *Prometheus) Use(e *gin.Engine) {
	e.Use(p.handlerFunc())
	e.GET(p.MetricsPath, prometheusHandler())
}

func (p *Prometheus) handlerFunc() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.String() == p.MetricsPath {
			c.Next()
			return
		}

		start := time.Now()

		reqSz := make(chan int)
		urlLen := 0
		if c.Request.URL != nil {
			urlLen = len(c.Request.URL.String())
		}
		go computeApproximateRequestSize(c.Request, reqSz, urlLen)

		c.Next()

		status := strconv.Itoa(c.Writer.Status())
		method := strings.ToLower(c.Request.Method)
		elapsed := time.Since(start).Seconds()
		resSz := float64(c.Writer.Size())

		splitName := strings.Split(c.HandlerName(), ".")
		handlerName := strings.TrimPrefix(splitName[len(splitName)-1], "Handle")

		p.reqDur.Observe(elapsed)
		p.reqCnt.WithLabelValues(status, method, handlerName).Inc()
		p.reqSz.Observe(float64(<-reqSz))
		p.resSz.Observe(resSz)
	}
}

func prometheusHandler() gin.HandlerFunc {
	h := prometheus.UninstrumentedHandler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

func computeApproximateRequestSize(r *http.Request, out chan int, s int) {
	s += len(r.Method)
	s += len(r.Proto)
	for name, values := range r.Header {
		s += len(name)
		for _, value := range values {
			s += len(value)
		}
	}
	s += len(r.Host)

	// N.B. r.Form and r.MultipartForm are assumed to be included in r.URL.

	if r.ContentLength != -1 {
		s += int(r.ContentLength)
	}
	out <- s
}
