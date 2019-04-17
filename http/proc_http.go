package http

import (
	"net/http"
	"strings"

	"github.com/open-falcon/query/graph"
	"github.com/open-falcon/query/proc"
)

func configProcHttpRoutes() {
	// TO BE DISCARDed
	http.HandleFunc("/statistics/all", func(w http.ResponseWriter, r *http.Request) {
		RenderDataJson(w, proc.GetAll())
	})

	// 获取所有计数器信息
	// counter
	http.HandleFunc("/counter/all", func(w http.ResponseWriter, r *http.Request) {
		RenderDataJson(w, proc.GetAll())
	})

	// 获取graph连接池计数器信息
	// conn pools
	http.HandleFunc("/proc/connpool", func(w http.ResponseWriter, r *http.Request) {
		result := strings.Join(graph.GraphConnPools.Proc(), "\n")
		w.Write([]byte(result))
	})
}
