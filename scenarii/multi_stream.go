/*
    Maxime Piraux's master's thesis
    Copyright (C) 2017-2018  Maxime Piraux

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU Affero General Public License version 3
	as published by the Free Software Foundation.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU Affero General Public License for more details.

    You should have received a copy of the GNU Affero General Public License
    along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package scenarii

import (
	m "github.com/mpiraux/master-thesis"
	"fmt"
	_ "github.com/davecgh/go-spew/spew"

	"time"
)

const (
	MS_TLSHandshakeFailed      = 1
	MS_NoTPReceived            = 2 // We don't distinguish the two first cases anymore
	MS_NotAllStreamsWereClosed = 3
)

type MultiStreamScenario struct {
	AbstractScenario
}

func NewMultiStreamScenario() *MultiStreamScenario {
	return &MultiStreamScenario{AbstractScenario{"multi_stream", 1, false, nil}}
}
func (s *MultiStreamScenario) Run(conn *m.Connection, trace *m.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)
	conn.TLSTPHandler.MaxData = 1024 * 1024
	conn.TLSTPHandler.MaxStreamData = 1024 * 1024 / 10

	allClosed := true
	connAgents := s.CompleteHandshake(conn, trace, MS_TLSHandshakeFailed)
	if connAgents == nil {
		return
	}
	defer connAgents.CloseConnection(false, 0, "")

	incPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incPackets)

	for i := uint16(0); i <= conn.TLSTPHandler.ReceivedParameters.MaxBidiStreams && i < 4; i++ {
		conn.SendHTTPGETRequest(preferredUrl, uint64(i*4))
	}

forLoop:
	for {
		select {
		case <-incPackets:
			for _, stream := range conn.Streams {
				if !stream.ReadClosed {
					allClosed = false
					break
				}
			}

			if allClosed {
				conn.CloseConnection(false, 0, "")
				break forLoop
			}
		case <-s.Timeout().C:
			break forLoop
		}
	}

	allClosed = true
	for streamId, stream := range conn.Streams {
		if streamId != 0 && !stream.ReadClosed {
			allClosed = false
			break
		}
	}

	if !allClosed {
		trace.ErrorCode = MS_NotAllStreamsWereClosed
		for streamId, stream := range conn.Streams {
			trace.Results[fmt.Sprintf("stream_%d_rec_offset", streamId)] = stream.ReadOffset
			trace.Results[fmt.Sprintf("stream_%d_snd_offset", streamId)] = stream.WriteOffset
			trace.Results[fmt.Sprintf("stream_%d_snd_closed", streamId)] = stream.WriteClosed
			trace.Results[fmt.Sprintf("stream_%d_rec_closed", streamId)] = stream.ReadClosed
		}
	}
}
