package wslt

import (
	"sync"
)

var (
	globalSession *Session

	onceSession = new(sync.Once)
)

func newSession() *Session {
	return &Session{
		mu:         sync.Mutex{},
		SID:        0,
		unUsedSids: make([]int64, 0),
		connectors: make(map[int64][]int64),
	}
}

//GlobalSession init globalSession
func GlobalSession() *Session {
	onceSession.Do(func() {
		globalSession = newSession()
	})
	return globalSession
}

type (
	Session struct {
		mu sync.Mutex

		SID int64 `json:"sid"`

		unUsedSids []int64

		//key = connectorID value = []sessionID
		connectors map[int64][]int64
	}
)

func (s *Session) SetConnectors(cid, sid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var (
		conSlice []int64
		ok       bool
	)
	if conSlice, ok = s.connectors[cid]; !ok {
		conSlice = []int64{sid}
	} else {
		conSlice = append(conSlice, sid)
	}
	s.connectors[cid] = conSlice
}

func (s *Session) UnUsedSids() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unUsedSids
}

func (s *Session) GetSidsByConnectorID(ctID int64) (sids []int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sids = make([]int64, 0)
	var (
		conSlice []int64
		ok       bool
	)
	if conSlice, ok = s.connectors[ctID]; !ok {
		return
	}
	sids = conSlice
	return
}

//OnWsClose while a ws conn closed, the sid minus one
func (s *Session) OnWsClose(cid, sid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unUsedSids = append(s.unUsedSids, sid)
	var (
		conSlice []int64
		ok       bool
	)
	if conSlice, ok = s.connectors[cid]; !ok {
		return
	}
	removeSliceInt64(&conSlice, sid)
	s.connectors[cid] = conSlice
}

//GetSid 获取sid,如果未使用区不为空,则使用未使用区第一个数字
func (s *Session) GetSid() (n int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.unUsedSids) > 0 {
		n = s.unUsedSids[0]
		s.unUsedSids = s.unUsedSids[1:len(s.unUsedSids)]
	} else {
		s.SID += 1
		n = s.SID
	}
	return
}
