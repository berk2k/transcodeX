package observability

import (
	"encoding/json"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"sync/atomic"
)

type Metrics struct {
	JobsReceived  atomic.Int64
	JobsCompleted atomic.Int64
	JobsFailed    atomic.Int64
	JobsRetried   atomic.Int64
}

var Global = &Metrics{}

func (m *Metrics) ServeJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int64{
		"jobs_received":  m.JobsReceived.Load(),
		"jobs_completed": m.JobsCompleted.Load(),
		"jobs_failed":    m.JobsFailed.Load(),
		"jobs_retried":   m.JobsRetried.Load(),
	})
}

func (m *Metrics) ServePrometheus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP transcodex_jobs_received Total jobs received\n")
	fmt.Fprintf(w, "# TYPE transcodex_jobs_received counter\n")
	fmt.Fprintf(w, "transcodex_jobs_received %d\n", m.JobsReceived.Load())

	fmt.Fprintf(w, "# HELP transcodex_jobs_completed Total jobs completed\n")
	fmt.Fprintf(w, "# TYPE transcodex_jobs_completed counter\n")
	fmt.Fprintf(w, "transcodex_jobs_completed %d\n", m.JobsCompleted.Load())

	fmt.Fprintf(w, "# HELP transcodex_jobs_failed Total jobs failed\n")
	fmt.Fprintf(w, "# TYPE transcodex_jobs_failed counter\n")
	fmt.Fprintf(w, "transcodex_jobs_failed %d\n", m.JobsFailed.Load())

	fmt.Fprintf(w, "# HELP transcodex_jobs_retried Total jobs retried\n")
	fmt.Fprintf(w, "# TYPE transcodex_jobs_retried counter\n")
	fmt.Fprintf(w, "transcodex_jobs_retried %d\n", m.JobsRetried.Load())
}

func StartServer(port string) {
	http.HandleFunc("/metrics/json", Global.ServeJSON)
	http.HandleFunc("/metrics", Global.ServePrometheus)
	go http.ListenAndServe(":"+port, nil)
}
