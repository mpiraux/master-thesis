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
	"bytes"

	"time"
	"github.com/mpiraux/master-thesis/agents"
)

const (
	UTS_NoConnectionCloseSent        = 1
	UTS_WrongErrorCodeIsUsed         = 2 // See https://tools.ietf.org/html/draft-ietf-quic-tls-10#section-11
	UTS_VNDidNotComplete             = 3
	UTS_ReceivedUnexpectedPacketType = 4
)

type UnsupportedTLSVersionScenario struct {
	AbstractScenario
}

func NewUnsupportedTLSVersionScenario() *UnsupportedTLSVersionScenario {
	return &UnsupportedTLSVersionScenario{AbstractScenario{"unsupported_tls_version", 1, false, nil}}
}
func (s *UnsupportedTLSVersionScenario) Run(conn *m.Connection, trace *m.Trace, preferredUrl string, debug bool) {
	s.timeout = time.NewTimer(10 * time.Second)
	connAgents := agents.AttachAgentsToConnection(conn, agents.GetDefaultAgents()...)
	connAgents.Get("TLSAgent").(*agents.TLSAgent).DisableFrameSending = true
	defer connAgents.StopAll()

	incPackets := make(chan interface{}, 1000)
	conn.IncomingPackets.Register(incPackets)

	sendUnsupportedInitial(conn)

	var connectionClosed bool
forLoop:
	for {
		select {
		case i := <-incPackets:
			switch p := i.(type) {
			case *m.VersionNegotationPacket:
				if err := conn.ProcessVersionNegotation(p); err != nil {
					trace.MarkError(UTS_VNDidNotComplete, err.Error(), p)
					return
				}
				sendUnsupportedInitial(conn)
			case m.Framer:
				for _, frame := range p.GetFrames() {
					if cc, ok := frame.(*m.ConnectionCloseFrame); ok { // See https://tools.ietf.org/html/draft-ietf-quic-tls-10#section-11
						if cc.ErrorCode != 0x201 {
							trace.MarkError(UTS_WrongErrorCodeIsUsed, "", p)
						}
						trace.Results["connection_reason_phrase"] = cc.ReasonPhrase
						connectionClosed = true
					}
				}
			case m.Packet:
				trace.MarkError(UTS_ReceivedUnexpectedPacketType, "", p)
			}
			case <-s.Timeout().C:
				break forLoop
		}
	}

	if !connectionClosed {
		trace.ErrorCode = UTS_NoConnectionCloseSent
	}
}

func sendUnsupportedInitial(conn *m.Connection) {
	initialPacket := conn.GetInitialPacket()
	for _, f := range initialPacket.Frames { // Advertise support of TLS 1.3 draft-00 only
		if frame, ok := f.(*m.CryptoFrame); ok {
			frame.CryptoData = bytes.Replace(frame.CryptoData, []byte{0x0, 0x2b, 0x0, 0x03, 0x2, 0x7f, 0x1c}, []byte{0x0, 0x2b, 0x0, 0x03, 0x2, 0x7f, 0x00}, 1)
		}
	}
	conn.SendPacket(initialPacket, m.EncryptionLevelInitial)
}
