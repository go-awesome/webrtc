package webrtc

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pions/sdp"
)

// Port of the SDPUtils logic used in
// https://github.com/webrtcHacks/adapter

// sdpParseRtpDecodingParameters parses the SDP media section and returns
// an array of RTCRtpDecodingParameters.
func sdpParseRtpDecodingParameters(d *sdp.MediaDescription) ([]RTCRtpDecodingParameters, error) {
	res := make([]RTCRtpDecodingParameters, 0)

	ssrcs := make([]sdpSsrcMedia, 0)
	err := sdpMatchAttributePrefixFunc(d, sdp.AttrKeySsrc, func(a sdp.Attribute) error {
		ssrc, pErr := sdpParseSsrcMedia(a)
		if pErr != nil {
			return pErr
		}
		// filter a=ssrc:... cname:, ignore PlanB-msid
		if ssrc.Attribute == "cname" {
			ssrcs = append(ssrcs, ssrc)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to parse ssrcs: %v", err)
	}

	if len(ssrcs) == 0 {
		return nil, errors.New("no ssrc found in media description")
	}

	primarySsrc := ssrcs[0].Ssrc

	res = append(res, RTCRtpDecodingParameters{
		RTCRtpCodingParameters{
			SSRC: primarySsrc,
		},
	})

	return res, nil
}

// Parses the SDP media section and returns RTCRtpParameters.
func sdpParseRtpParameters(d *sdp.MediaDescription) (RTCRtpParameters, error) {
	res := RTCRtpParameters{}

	// Parse codecs
	for _, format := range d.MediaName.Formats {
		prefix := fmt.Sprintf("rtpmap:%s ", format)
		err := sdpMatchAttributePrefixFunc(d, prefix, sdpRtpCodecParser(d, &res))
		if err != nil {
			return RTCRtpParameters{}, fmt.Errorf("failed to parse codec: %v", err)
		}
	}

	// Parse header extensions
	err := sdpMatchAttributePrefixFunc(d, "extmap:", sdpExtmapParser(d, &res))
	if err != nil {
		return RTCRtpParameters{}, fmt.Errorf("failed to parse header extensions: %v", err)
	}

	return res, nil
}

func sdpRtpCodecParser(d *sdp.MediaDescription, p *RTCRtpParameters) sdpAttributeParser {
	return func(a sdp.Attribute) error {
		codec, err := sdpParseRtpMap(a)
		if err != nil {
			return err
		}

		prefix := fmt.Sprintf("fmtp:%d ", codec.PayloadType)
		err = sdpMatchAttributePrefixFunc(d, prefix, sdpRtpFmtpParser(d, &codec))
		if err != nil {
			return fmt.Errorf("failed to parse fmtp: %v", err)
		}

		prefix = fmt.Sprintf("rtcp-fb:%d ", codec.PayloadType)
		err = sdpMatchAttributePrefixFunc(d, prefix, sdpRtpRtcpFeedbackParser(d, &codec))
		if err != nil {
			return fmt.Errorf("failed to parse rtcp feedback: %v", err)
		}

		p.Codecs = append(p.Codecs, codec)

		return nil
	}
}

func sdpRtpFmtpParser(d *sdp.MediaDescription, p *RTCRtpCodecParameters) sdpAttributeParser {
	return func(a sdp.Attribute) error {
		params, err := sdpParseFmtp(a)
		if err != nil {
			return err
		}
		p.Parameters = params

		return nil
	}
}

// Parses an ftmp line, returns dictionary. Sample input:
// a=fmtp:96 vbr=on;cng=on
// Also deals with vbr=on; cng=on
func sdpParseFmtp(a sdp.Attribute) (map[string]string, error) {
	sp := strings.Index(a.Value, " ")
	if sp < 5 {
		return nil, fmt.Errorf("attribute to short: %s", a.Value)
	}

	paramsStr := a.Value[sp+1:]
	return sdpParseFmtpString(paramsStr)
}

// Parses an ftmp value, returns dictionary. Sample input:
// vbr=on;cng=on
func sdpParseFmtpString(paramsStr string) (map[string]string, error) {
	res := make(map[string]string)

	params := strings.Split(paramsStr, ";")
	for _, param := range params {
		parts := strings.Split(param, "=")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid parameter: %s", param)
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		res[k] = v
	}

	return res, nil
}

func sdpRtpRtcpFeedbackParser(d *sdp.MediaDescription, p *RTCRtpCodecParameters) sdpAttributeParser {
	return func(a sdp.Attribute) error {
		fb, err := sdpParseRtcpFeedback(a)
		if err != nil {
			return err
		}
		p.RtcpFeedback = append(p.RtcpFeedback, fb)

		return nil
	}
}

// Parses an rtcp-fb line, returns RTCPRtcpFeedback object. Sample input:
// a=rtcp-fb:98 nack rpsi
func sdpParseRtcpFeedback(a sdp.Attribute) (RTCRtcpFeedback, error) {
	sp := strings.Index(a.Value, " ")
	if sp < 8 {
		return RTCRtcpFeedback{}, fmt.Errorf("rtcp-fb attribute to short: %s", a.Value)
	}
	fbStr := a.Value[sp+1:]
	sp = strings.Index(fbStr, " ")

	typ := fbStr
	param := ""
	if sp > 0 {
		typ = fbStr[:sp]
		param = fbStr[sp+1:]
	}

	return RTCRtcpFeedback{
		Type:      typ,
		Parameter: param,
	}, nil
}

func sdpExtmapParser(d *sdp.MediaDescription, p *RTCRtpParameters) sdpAttributeParser {
	return func(a sdp.Attribute) error {
		ext, err := sdpParseExtmap(a)
		if err != nil {
			return err
		}
		p.HeaderExtensions = append(p.HeaderExtensions, ext)

		return nil
	}
}

// Parses an a=extmap line (headerextension from RFC 5285). Sample input:
// a=extmap:2 urn:ietf:params:rtp-hdrext:toffset
// a=extmap:2/sendonly urn:ietf:params:rtp-hdrext:toffset
func sdpParseExtmap(a sdp.Attribute) (RTCRtpHeaderExtensionParameters, error) {
	sp := strings.Index(a.Value, " ")
	if sp < 7 {
		return RTCRtpHeaderExtensionParameters{}, fmt.Errorf("extmap attribute to short: %s", a.Value)
	}

	idParts := strings.Split(a.Value[7:sp], "/")

	idStr := idParts[0]
	id, err := strconv.ParseUint(idStr, 10, 16)
	if err != nil {
		return RTCRtpHeaderExtensionParameters{}, fmt.Errorf("invalid id: %s", idStr)
	}

	direction := "sendrecv"
	if len(idParts) > 1 {
		direction = idParts[1]
	}

	uri := a.Value[sp+1:]

	return RTCRtpHeaderExtensionParameters{
		ID:        uint16(id),
		direction: direction,
		URI:       uri,
	}, nil
}

// Parses an rtpmap line, returns RTCRtpCoddecParameters. Sample input:
// a=rtpmap:109 opus/48000/2
func sdpParseRtpMap(a sdp.Attribute) (RTCRtpCodecParameters, error) {
	sp := strings.Index(a.Value, " ")
	if sp < 7 {
		return RTCRtpCodecParameters{}, fmt.Errorf("rtpmap attribute to short: %s", a.Value)
	}
	payloadTypeStr := a.Value[7:sp]
	payloadTypeRaw, err := strconv.ParseUint(payloadTypeStr, 10, 8)
	if err != nil {
		return RTCRtpCodecParameters{}, fmt.Errorf("invalid payload type: %s", payloadTypeStr)
	}

	codecStr := a.Value[sp+1:]
	parts := strings.Split(codecStr, "/")
	if len(parts) < 2 {
		return RTCRtpCodecParameters{}, fmt.Errorf("invalid codec: %s", codecStr)
	}
	name := parts[0]

	clockrateStr := parts[1]
	clockrate, err := strconv.ParseUint(clockrateStr, 10, 32)
	if err != nil {
		return RTCRtpCodecParameters{}, fmt.Errorf("invalid clockrate: %s", clockrateStr)
	}

	channels := uint64(0)
	if len(parts) == 3 {
		channelsStr := parts[2]
		channels, err = strconv.ParseUint(channelsStr, 10, 32)
		if err != nil {
			return RTCRtpCodecParameters{}, fmt.Errorf("invalid channels: %s", channelsStr)
		}
	}

	return RTCRtpCodecParameters{
		Name:        name,
		PayloadType: uint8(payloadTypeRaw),
		ClockRate:   uint32(clockrate),
		Channels:    uint32(channels),
	}, nil
}

// Generic: could be moved to package SDP

// sdpSsrcMedia represents a an RFC 5576 ssrc media attribute.
type sdpSsrcMedia struct {
	Ssrc      uint32
	Attribute string
	Value     string
}

// Parses an RFC 5576 ssrc media attribute. Sample input:
// a=ssrc:<ssrc-id> <attribute>
// a=ssrc:<ssrc-id> <attribute>:<value>
func sdpParseSsrcMedia(a sdp.Attribute) (sdpSsrcMedia, error) {
	sp := strings.Index(a.Value, " ")
	if sp < 5 {
		return sdpSsrcMedia{}, fmt.Errorf("ssrc media attribute to short: %s", a.Value)
	}
	ssrcStr := a.Value[5:sp]
	ssrc, err := strconv.ParseUint(ssrcStr, 10, 32)
	if err != nil {
		return sdpSsrcMedia{}, fmt.Errorf("failed to parse ssrc: %s", ssrcStr)
	}
	parts := strings.Split(a.Value[sp+1:], ":")
	attribute := parts[0]
	value := ""
	if len(parts) > 1 {
		value = parts[1]
	}

	return sdpSsrcMedia{
		Ssrc:      uint32(ssrc),
		Attribute: attribute,
		Value:     value,
	}, nil
}

type sdpAttributeParser func(sdp.Attribute) error

func sdpMatchAttributePrefixFunc(d *sdp.MediaDescription, prefix string, p sdpAttributeParser) error {
	for _, a := range d.Attributes {
		if strings.HasPrefix(a.Value, prefix) {
			err := p(a)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
