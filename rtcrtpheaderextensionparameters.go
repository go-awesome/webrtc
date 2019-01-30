package webrtc

// RTCRtpHeaderExtensionParameters dictionary enables a header extension
// to be configured for use within an RTCRtpSender or RTCRtpReceiver.
type RTCRtpHeaderExtensionParameters struct {
	ID        uint16
	direction string
	URI       string
}
