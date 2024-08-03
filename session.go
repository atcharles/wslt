package wslt

import (
	"sync"
)

var (
	globalSession *Session

	onceSession = new(sync.Once)
)

type (
	SessionRelationShip struct {
		connectorID int64
		sids        []string
	}

	SessionData struct {
		ConnectorID int64  `json:"connector_id,omitempty"`
		SessionID   string `json:"session_id,omitempty"`
	}

	Session struct {
		mu sync.RWMutex

		removeChan chan *SessionData

		addChan chan *SessionData

		connectors map[int64][]string
	}
)

func (s *Session) GetSidsByConnectorID(ctID int64) (sids []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conSlice, ok := s.connectors[ctID]
	if !ok {
		return
	}
	sids = make([]string, len(conSlice))
	copy(sids, conSlice)
	return
}

func (s *Session) listen() {
	for {
		select {
		case rs, ok := <-s.removeChan:
			if !ok {
				return
			}
			sids, has := s.connectors[rs.ConnectorID]
			if !has {
				continue
			}
			for i, sid := range sids {
				if sid == rs.SessionID {
					sids = append(sids[:i], sids[i+1:]...)
					break
				}
			}
			s.connectors[rs.ConnectorID] = sids
		case session, ok := <-s.addChan:
			if !ok {
				return
			}
			sids, has := s.connectors[session.ConnectorID]
			if !has {
				sids = make([]string, 0)
			}
			sids = append(sids, session.SessionID)
			s.connectors[session.ConnectorID] = sids
		}
	}
}

// GlobalSession init globalSession
func GlobalSession() *Session {
	onceSession.Do(func() {
		globalSession = newSession()
	})
	return globalSession
}

func newSession() *Session {
	sn := &Session{
		mu:         sync.RWMutex{},
		removeChan: make(chan *SessionData, 1024),
		connectors: make(map[int64][]string),
		addChan:    make(chan *SessionData, 1),
	}
	go sn.listen()
	return sn
}
