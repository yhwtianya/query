package graph

import (
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	cmodel "github.com/open-falcon/common/model"
	cutils "github.com/open-falcon/common/utils"
	rings "github.com/toolkits/consistent/rings"
	nset "github.com/toolkits/container/set"
	spool "github.com/toolkits/pool/simple_conn_pool"

	"github.com/open-falcon/query/g"
)

// 连接池
// node_address -> connection_pool
var (
	GraphConnPools *spool.SafeRpcConnPools
)

// 服务节点的一致性哈希环
// pk -> node
var (
	GraphNodeRing *rings.ConsistentHashNodeRing
)

// 初始化graph 哈希环和连接池
func Start() {
	initNodeRings()
	initConnPools()
	log.Println("graph.Start ok")
}

// 查询单指标历史数据
func QueryOne(para cmodel.GraphQueryParam) (resp *cmodel.GraphQueryResponse, err error) {
	start, end := para.Start, para.End
	endpoint, counter := para.Endpoint, para.Counter

	// 获取连接池
	pool, addr, err := selectPool(endpoint, counter)
	if err != nil {
		return nil, err
	}

	// 获取连接
	conn, err := pool.Fetch()
	if err != nil {
		return nil, err
	}

	rpcConn := conn.(spool.RpcClient)
	if rpcConn.Closed() {
		pool.ForceClose(conn)
		return nil, errors.New("conn closed")
	}

	type ChResult struct {
		Err  error
		Resp *cmodel.GraphQueryResponse
	}

	ch := make(chan *ChResult, 1)
	go func() {
		resp := &cmodel.GraphQueryResponse{}
		// 转发到实际的graph进行查询
		err := rpcConn.Call("Graph.Query", para, resp)
		ch <- &ChResult{Err: err, Resp: resp}
	}()

	select {
	case <-time.After(time.Duration(g.Config().Graph.CallTimeout) * time.Millisecond):
		pool.ForceClose(conn)
		return nil, fmt.Errorf("%s, call timeout. proc: %s", addr, pool.Proc())
	case r := <-ch:
		if r.Err != nil {
			pool.ForceClose(conn)
			return r.Resp, fmt.Errorf("%s, call failed, err %v. proc: %s", addr, r.Err, pool.Proc())
		} else {
			pool.Release(conn)

			// 没有值
			if len(r.Resp.Values) < 1 {
				return r.Resp, nil
			}

			// 修正数据
			// TODO query不该做这些事情, 说明graph没做好
			fixed := []*cmodel.RRDData{}
			for _, v := range r.Resp.Values {
				if v == nil || !(v.Timestamp >= start && v.Timestamp <= end) {
					continue
				}
				//FIXME: 查询数据的时候，把所有的负值都过滤掉，因为transfer之前在设置最小值的时候为U
				if (r.Resp.DsType == "DERIVE" || r.Resp.DsType == "COUNTER") && v.Value < 0 {
					fixed = append(fixed, &cmodel.RRDData{Timestamp: v.Timestamp, Value: cmodel.JsonFloat(math.NaN())})
				} else {
					fixed = append(fixed, v)
				}
			}
			r.Resp.Values = fixed
		}
		return r.Resp, nil
	}
}

// 获取指标及其rrd属性
func Info(para cmodel.GraphInfoParam) (resp *cmodel.GraphFullyInfo, err error) {
	endpoint, counter := para.Endpoint, para.Counter

	pool, addr, err := selectPool(endpoint, counter)
	if err != nil {
		return nil, err
	}

	conn, err := pool.Fetch()
	if err != nil {
		return nil, err
	}

	rpcConn := conn.(spool.RpcClient)
	if rpcConn.Closed() {
		pool.ForceClose(conn)
		return nil, errors.New("conn closed")
	}

	type ChResult struct {
		Err  error
		Resp *cmodel.GraphInfoResp
	}
	ch := make(chan *ChResult, 1)
	go func() {
		resp := &cmodel.GraphInfoResp{}
		err := rpcConn.Call("Graph.Info", para, resp)
		ch <- &ChResult{Err: err, Resp: resp}
	}()

	select {
	case <-time.After(time.Duration(g.Config().Graph.CallTimeout) * time.Millisecond):
		pool.ForceClose(conn)
		return nil, fmt.Errorf("%s, call timeout. proc: %s", addr, pool.Proc())
	case r := <-ch:
		if r.Err != nil {
			pool.ForceClose(conn)
			return nil, fmt.Errorf("%s, call failed, err %v. proc: %s", addr, r.Err, pool.Proc())
		} else {
			pool.Release(conn)
			fullyInfo := cmodel.GraphFullyInfo{
				Endpoint:  endpoint,
				Counter:   counter,
				ConsolFun: r.Resp.ConsolFun,
				Step:      r.Resp.Step,
				Filename:  r.Resp.Filename,
				Addr:      addr,
			}
			return &fullyInfo, nil
		}
	}
}

// 获取指标最新数据,对于DERIVE和COUNTER类型的指标，为计算后的值
func Last(para cmodel.GraphLastParam) (r *cmodel.GraphLastResp, err error) {
	endpoint, counter := para.Endpoint, para.Counter

	pool, addr, err := selectPool(endpoint, counter)
	if err != nil {
		return nil, err
	}

	conn, err := pool.Fetch()
	if err != nil {
		return nil, err
	}

	rpcConn := conn.(spool.RpcClient)
	if rpcConn.Closed() {
		pool.ForceClose(conn)
		return nil, errors.New("conn closed")
	}

	type ChResult struct {
		Err  error
		Resp *cmodel.GraphLastResp
	}
	ch := make(chan *ChResult, 1)
	go func() {
		resp := &cmodel.GraphLastResp{}
		err := rpcConn.Call("Graph.Last", para, resp)
		ch <- &ChResult{Err: err, Resp: resp}
	}()

	select {
	case <-time.After(time.Duration(g.Config().Graph.CallTimeout) * time.Millisecond):
		pool.ForceClose(conn)
		return nil, fmt.Errorf("%s, call timeout. proc: %s", addr, pool.Proc())
	case r := <-ch:
		if r.Err != nil {
			pool.ForceClose(conn)
			return r.Resp, fmt.Errorf("%s, call failed, err %v. proc: %s", addr, r.Err, pool.Proc())
		} else {
			pool.Release(conn)
			return r.Resp, nil
		}
	}
}

// 获取指标最新原始上报数据
func LastRaw(para cmodel.GraphLastParam) (r *cmodel.GraphLastResp, err error) {
	endpoint, counter := para.Endpoint, para.Counter

	pool, addr, err := selectPool(endpoint, counter)
	if err != nil {
		return nil, err
	}

	conn, err := pool.Fetch()
	if err != nil {
		return nil, err
	}
	rpcConn := conn.(spool.RpcClient)
	if rpcConn.Closed() {
		pool.ForceClose(conn)
		return nil, errors.New("conn closed")
	}

	type ChResult struct {
		Err  error
		Resp *cmodel.GraphLastResp
	}
	ch := make(chan *ChResult, 1)
	go func() {
		resp := &cmodel.GraphLastResp{}
		err := rpcConn.Call("Graph.LastRaw", para, resp)
		ch <- &ChResult{Err: err, Resp: resp}
	}()

	select {
	case <-time.After(time.Duration(g.Config().Graph.CallTimeout) * time.Millisecond):
		pool.ForceClose(conn)
		return nil, fmt.Errorf("%s, call timeout. proc: %s", addr, pool.Proc())
	case r := <-ch:
		if r.Err != nil {
			pool.ForceClose(conn)
			return r.Resp, fmt.Errorf("%s, call failed, err %v. proc: %s", addr, r.Err, pool.Proc())
		} else {
			pool.Release(conn)
			return r.Resp, nil
		}
	}
}

// 从哈希环获取节点地址，根据节点地址从连接池管理器获取对应连接池
func selectPool(endpoint, counter string) (rpool *spool.ConnPool, raddr string, rerr error) {
	pkey := cutils.PK2(endpoint, counter)
	node, err := GraphNodeRing.GetNode(pkey)
	if err != nil {
		return nil, "", err
	}

	addr, found := g.Config().Graph.Cluster[node]
	if !found {
		return nil, "", errors.New("node not found")
	}

	pool, found := GraphConnPools.Get(addr)
	if !found {
		return nil, addr, errors.New("addr not found")
	}

	return pool, addr, nil
}

// 初始化graph rpc连接池
// internal functions
func initConnPools() {
	cfg := g.Config()

	// TODO 为了得到Slice,这里做的太复杂了
	graphInstances := nset.NewSafeSet()
	for _, address := range cfg.Graph.Cluster {
		// 地址去重
		graphInstances.Add(address)
	}
	// 构建graph连接池管理器
	GraphConnPools = spool.CreateSafeRpcConnPools(cfg.Graph.MaxConns, cfg.Graph.MaxIdle,
		cfg.Graph.ConnTimeout, cfg.Graph.CallTimeout, graphInstances.ToSlice())
}

// 初始化graph节点哈希环
func initNodeRings() {
	cfg := g.Config()
	GraphNodeRing = rings.NewConsistentHashNodesRing(cfg.Graph.Replicas, cutils.KeysOfMap(cfg.Graph.Cluster))
}
