package webrtc

import (
	"fmt"

	"github.com/pions/rtcp"
	"github.com/pions/rtp"
	"github.com/pions/srtp"
)

// RTCRtpReceiver allows an application to inspect the receipt of a RTCTrack
type RTCRtpReceiver struct {
	kind      RTCRtpCodecType
	transport *RTCDtlsTransport

	hasRecv chan bool

	Track *RTCTrack

	rtpOut  chan *rtp.Packet
	rtcpOut chan rtcp.Packet

	// A reference to the associated api object
	api *API
}

// NewRTCRtpReceiver constructs a new RTCRtpReceiver
func (api *API) NewRTCRtpReceiver(kind RTCRtpCodecType, transport *RTCDtlsTransport) *RTCRtpReceiver {
	return &RTCRtpReceiver{
		kind:      kind,
		transport: transport,
		hasRecv:   make(chan bool),
		rtpOut:    make(chan *rtp.Packet, 15),
		rtcpOut:   make(chan rtcp.Packet, 15),
		api:       api,
	}
}

// Receive blocks until the RTCTrack is available
func (r *RTCRtpReceiver) Receive(parameters RTCRtpReceiveParameters) error {
	// TODO atomic only allow this to fire once
	ssrc := parameters.Encodings[0].SSRC

	srtpSession := r.transport.srtpSession
	readStreamRTP, err := srtpSession.OpenReadStream(ssrc)
	if err != nil {
		return fmt.Errorf("failed to open RTP ReadStream %d: %v", ssrc, err)
	}

	srtcpSession := r.transport.srtcpSession
	readStreamRTCP, err := srtcpSession.OpenReadStream(ssrc)
	if err != nil {
		return fmt.Errorf("failed to open RTCP ReadStream %d: %v", ssrc, err)
	}

	// Start readloops
	recvLoopRTP, payloadTypeCh := r.createRecvLoopRTP()

	go recvLoopRTP(readStreamRTP, ssrc)
	go r.recvLoopRTCP(readStreamRTCP, ssrc)

	payloadType := <-payloadTypeCh
	codecParams, err := parameters.getCodecParameters(payloadType)
	if err != nil {
		return fmt.Errorf("failed to find codec parameters: %v", err)
	}

	codec, err := r.api.mediaEngine.getCodecSDP(codecParams)
	if err != nil {
		return fmt.Errorf("codec %s in not registered", codecParams)
	}

	// Set the receiver track
	r.Track = &RTCTrack{
		PayloadType: payloadType,
		Kind:        codec.Type,
		Codec:       codec,
		Ssrc:        ssrc,
		Packets:     r.rtpOut,
		RTCPPackets: r.rtcpOut,
	}

	return nil
}

func (r *RTCRtpReceiver) createRecvLoopRTP() (func(stream *srtp.ReadStreamSRTP, ssrc uint32), chan uint8) {
	payloadTypeCh := make(chan uint8)
	return func(stream *srtp.ReadStreamSRTP, ssrc uint32) {
		readBuf := make([]byte, receiveMTU)
		for {
			rtpLen, err := stream.Read(readBuf)
			if err != nil {
				pcLog.Warnf("Failed to read, RTCTrack done for: %v %d \n", err, ssrc)
				return
			}

			var rtpPacket rtp.Packet
			if err = rtpPacket.Unmarshal(append([]byte{}, readBuf[:rtpLen]...)); err != nil {
				pcLog.Warnf("Failed to unmarshal RTP packet, discarding: %v \n", err)
				continue
			}

			select {
			case payloadTypeCh <- rtpPacket.PayloadType:
				payloadTypeCh = nil
			case r.rtpOut <- &rtpPacket:
			default:
			}
		}
	}, payloadTypeCh
}

func (r *RTCRtpReceiver) recvLoopRTCP(stream *srtp.ReadStreamSRTCP, ssrc uint32) {
	readBuf := make([]byte, receiveMTU)
	for {
		rtcpLen, err := stream.Read(readBuf)
		if err != nil {
			pcLog.Warnf("Failed to read, RTCTrack done for: %v %d \n", err, ssrc)
			return
		}

		rtcpPacket, _, err := rtcp.Unmarshal(append([]byte{}, readBuf[:rtcpLen]...))
		if err != nil {
			pcLog.Warnf("Failed to unmarshal RTCP packet, discarding: %v \n", err)
			continue
		}

		select {
		case r.rtcpOut <- rtcpPacket:
		default:
		}
	}
}

// Stop irreversibly stops the RTCRtpReceiver
func (r *RTCRtpReceiver) Stop() {
	// TODO properly tear down all loops (and test that)
}
