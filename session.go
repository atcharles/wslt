package wslt

import (
	"sync"
)

var (
	globalSession *Session

	onceSession = new(sync.Once)
)

func newSession() *Session {
	sn := &Session{
		mu:         sync.Mutex{},
		SID:        0,
		removeChan: make(chan *SessionRelationShip, 1024),
		unusedSids: make([]int64, 0),
		connectors: make(map[int64][]int64),
		addChan:    make(chan int64, 1),
		sidChan:    make(chan int64, 1),
	}
	go sn.listen()
	return sn
}

//GlobalSession init globalSession
func GlobalSession() *Session {
	onceSession.Do(func() {
		globalSession = newSession()
	})
	return globalSession
}

type (
	SessionRelationShip struct {
		connectorID int64
		sids        []int64
	}

	Session struct {
		mu sync.Mutex

		SID int64 `json:"sid"`
		//this session id is unused
		removeChan chan *SessionRelationShip

		unusedSids []int64

		connectors map[int64][]int64

		addChan chan int64

		sidChan chan int64
	}
)

func (s *Session) listen() {
	for {
		select {
		case rs := <-s.removeChan:
			StdLogger.Printf("remove chan:%#v\n", rs)
			s.unusedSids = append(s.unusedSids, rs.sids...)
			var sids []int64
			var has bool
			if sids, has = s.connectors[rs.connectorID]; !has {
				continue
			}
			for _, sid := range rs.sids {
				removeSliceInt64(&sids, sid)
			}
			s.connectors[rs.connectorID] = sids
		case connectorID := <-s.addChan:
			var n int64
			if len(s.unusedSids) > 0 {
				n = s.unusedSids[0]
				s.unusedSids = s.unusedSids[1:len(s.unusedSids)]
			} else {
				s.SID += 1
				n = s.SID
			}
			var sids []int64
			var has bool
			if sids, has = s.connectors[connectorID]; !has {
				sids = []int64{n}
			} else {
				sids = append(sids, n)
			}
			s.connectors[connectorID] = sids
			s.sidChan <- n
			StdLogger.Printf("addChan connectorID:%d, chan:%d\n", connectorID, n)
		}
	}
}

func (s *Session) UnUsedSids() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unusedSids
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
