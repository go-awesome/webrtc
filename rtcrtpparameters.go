package webrtc

import "fmt"

// RTCRtpParameters contains the RTP stack settings used by both senders and receivers.
type RTCRtpParameters struct {
	Codecs           []RTCRtpCodecParameters
	HeaderExtensions []RTCRtpHeaderExtensionParameters
	Rtcp             RTCRtcpParameters
}

func (p RTCRtpReceiveParameters) getCodecParameters(payloadType uint8) (RTCRtpCodecParameters, error) {
	for _, codec := range p.Codecs {
		if codec.PayloadType == payloadType {
			return codec, nil
		}
	}

	return RTCRtpCodecParameters{}, fmt.Errorf("payload type %d not found", payloadType)
}
