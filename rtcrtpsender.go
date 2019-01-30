package webrtc

import (
	"fmt"
	"sync"

	"github.com/pions/rtcp"
	"github.com/pions/rtp"
	"github.com/pions/srtp"
	"github.com/pions/webrtc/pkg/media"
)

const rtpOutboundMTU = 1400

// RTCRtpSender allows an application to control how a given RTCTrack is encoded and transmitted to a remote peer
type RTCRtpSender struct {
	lock sync.RWMutex

	Track *RTCTrack

	transport *RTCDtlsTransport

	// A reference to the associated api object
	api *API
}

// NewRTCRtpSender constructs a new RTCRtpSender
func (api *API) NewRTCRtpSender(track *RTCTrack, transport *RTCDtlsTransport) *RTCRtpSender {
	return &RTCRtpSender{
		Track:     track,
		transport: transport,
		api:       api,
	}
}

// Send Attempts to set the parameters controlling the sending of media.
func (r *RTCRtpSender) Send(parameters RTCRtpSendParameters) error {
	sampleInput := make(chan media.RTCSample, 15) // Is the buffering needed?
	rawInput := make(chan *rtp.Packet, 15)        // Is the buffering needed?
	rtcpInput := make(chan rtcp.Packet, 15)       // Is the buffering needed?

	r.Track.Samples = sampleInput
	r.Track.RawRTP = rawInput
	r.Track.RTCPPackets = rtcpInput

	if r.Track.isRawRTP {
		close(r.Track.Samples)
		go r.handleRawRTP(rawInput)
	} else {
		close(r.Track.RawRTP)
		go r.handleSampleRTP(sampleInput)
	}

	dtls, err := r.Transport()
	if err != nil {
		return err
	}

	srtcpSession, err := dtls.getSrtcpSession()
	if err != nil {
		return err
	}

	ssrc := r.Track.Ssrc
	srtcpStream, err := srtcpSession.OpenReadStream(ssrc)
	if err != nil {
		return fmt.Errorf("failed to open RTCP ReadStream, RTCTrack done for: %v %d", err, ssrc)
	}

	go r.handleRTCP(srtcpStream, rtcpInput)

	return nil
}

// Stop irreversibly stops the RTCRtpSender
func (r *RTCRtpSender) Stop() {
	if r.Track.isRawRTP {
		close(r.Track.RawRTP)
	} else {
		close(r.Track.Samples)
	}

	// TODO properly tear down all loops (and test that)
}

func (r *RTCRtpSender) handleRawRTP(rtpPackets chan *rtp.Packet) {
	for {
		p, ok := <-rtpPackets
		if !ok {
			return
		}

		err := r.sendRTP(p)
		if err != nil {
			pcLog.Warnf("failed to send RTP: %v", err)
		}
	}
}

func (r *RTCRtpSender) handleSampleRTP(rtpPackets chan media.RTCSample) {
	packetizer := rtp.NewPacketizer(
		rtpOutboundMTU,
		r.Track.PayloadType,
		r.Track.Ssrc,
		r.Track.Codec.Payloader,
		rtp.NewRandomSequencer(),
		r.Track.Codec.ClockRate,
	)

	for {
		in, ok := <-rtpPackets
		if !ok {
			return
		}
		packets := packetizer.Packetize(in.Data, in.Samples)
		for _, p := range packets {
			err := r.sendRTP(p)
			if err != nil {
				pcLog.Warnf("failed to send RTP: %v", err)
			}
		}
	}

}

// Transport returns the RTCDtlsTransport instance over which
// RTP is sent and received.
func (r *RTCRtpSender) Transport() (*RTCDtlsTransport, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	if r.transport == nil {
		return nil, fmt.Errorf("the DTLS transport is not started")
	}

	return r.transport, nil
}

func (r *RTCRtpSender) handleRTCP(stream *srtp.ReadStreamSRTCP, rtcpPackets chan rtcp.Packet) {
	var rtcpPacket rtcp.Packet
	for {
		rtcpBuf := make([]byte, receiveMTU)
		i, err := stream.Read(rtcpBuf)
		if err != nil {
			pcLog.Warnf("Failed to read, RTCTrack done for: %v %d \n", err, r.Track.Ssrc)
			return
		}

		rtcpPacket, _, err = rtcp.Unmarshal(rtcpBuf[:i])
		if err != nil {
			pcLog.Warnf("Failed to unmarshal RTCP packet, discarding: %v \n", err)
			continue
		}

		select {
		case rtcpPackets <- rtcpPacket:
		default:
		}
	}
}

func (r *RTCRtpSender) sendRTP(packet *rtp.Packet) error {
	dtls, err := r.Transport()
	if err != nil {
		return err
	}

	srtpSession, err := dtls.getSrtpSession()
	if err != nil {
		return err
	}

	writeStream, err := srtpSession.OpenWriteStream()
	if err != nil {
		return fmt.Errorf("failed to open WriteStream: %v", err)
	}

	if _, err := writeStream.WriteRTP(&packet.Header, packet.Payload); err != nil {
		return fmt.Errorf("failed to write: %v", err)
	}

	return nil
}
