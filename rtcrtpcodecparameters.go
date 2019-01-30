package webrtc

import "fmt"

// RTCRtpCodecParameters provides information on codec settings.
type RTCRtpCodecParameters struct {
	Name         string
	MimeType     string
	PayloadType  uint8
	ClockRate    uint32
	Maxptime     uint32
	Ptime        uint32
	Channels     uint32
	RtcpFeedback []RTCRtcpFeedback
	Parameters   map[string]string
}

func (p RTCRtpCodecParameters) String() string {
	return fmt.Sprintf("%d %s/%d/%d", p.PayloadType, p.Name, p.ClockRate, p.Channels)
}

func (p RTCRtpCodecParameters) equalFMTP(other string) (bool, error) {
	b, err := sdpParseFmtpString(other)
	if err != nil {
		return false, fmt.Errorf("failed to parse FMTP: %v", err)
	}

	return cmpMapStringString(p.Parameters, b), nil
}

func cmpMapStringString(a, b map[string]string) bool {
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}

	for k, v := range a {
		vb, ok := b[k]
		if !ok || vb != v {
			return false
		}
	}

	return true
}
