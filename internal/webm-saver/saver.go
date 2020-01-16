package webm_saver

import (
	"fmt"
	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v2"
	"github.com/pion/webrtc/v2/pkg/media"
	"github.com/pion/webrtc/v2/pkg/media/samplebuilder"
	"math/rand"
	"os"
)

// pion/webrtc/track.go
const (
	rtpOutboundMTU          = 1200
	trackDefaultIDLength    = 16
	trackDefaultLabelLength = 16
)

type WebmSaver struct {
	path string
	audioWriter, videoWriter       webm.BlockWriteCloser
	audioBuilder, videoBuilder     *samplebuilder.SampleBuilder
	audioTimestamp, videoTimestamp uint32
	packetizer rtp.Packetizer
}

func NewWebmSaver(path string) *WebmSaver {
	codec := webrtc.NewRTPVP8Codec(webrtc.DefaultPayloadTypeVP8, 90000)
	packetizer := rtp.NewPacketizer(
		rtpOutboundMTU,
		codec.PayloadType,
		rand.Uint32(),
		codec.Payloader,
		rtp.NewRandomSequencer(),
		codec.ClockRate,
	)

	return &WebmSaver{
		path: path,
		audioBuilder: samplebuilder.New(10, &codecs.OpusPacket{}),
		videoBuilder: samplebuilder.New(10, &codecs.VP8Packet{}),
		packetizer: packetizer,
	}
}

func (s *WebmSaver) Close() {
	fmt.Printf("Finalizing webm...\n")
	if s.audioWriter != nil {
		if err := s.audioWriter.Close(); err != nil {
			panic(err)
		}
	}
	if s.videoWriter != nil {
		if err := s.videoWriter.Close(); err != nil {
			panic(err)
		}
	}
}
func (s *WebmSaver) PushOpus(rtpPacket *rtp.Packet) {
	s.audioBuilder.Push(rtpPacket)

	for {
		sample := s.audioBuilder.Pop()
		if sample == nil {
			return
		}
		if s.audioWriter != nil {
			s.audioTimestamp += sample.Samples
			t := s.audioTimestamp / 48
			if _, err := s.audioWriter.Write(true, int64(t), rtpPacket.Payload); err != nil {
				panic(err)
			}
		}
	}
}
func (s *WebmSaver) PushVP8(rtpPacket *rtp.Packet) {
	s.videoBuilder.Push(rtpPacket)

	for {
		sample := s.videoBuilder.Pop()
		//fmt.Printf("WebmSaver#PushVP8 | pop:%v \n", sample)
		if sample == nil {
			return
		}
		// Read VP8 header.
		videoKeyframe := (sample.Data[0]&0x1 == 0)
		if videoKeyframe {
			fmt.Printf("WebmSaver#PushVP8 | videoKeyframe:%v \n", videoKeyframe)
			// Keyframe has frame information.
			raw := uint(sample.Data[6]) | uint(sample.Data[7])<<8 | uint(sample.Data[8])<<16 | uint(sample.Data[9])<<24
			width := int(raw & 0x3FFF)
			height := int((raw >> 16) & 0x3FFF)

			if s.videoWriter == nil || s.audioWriter == nil {
				// Initialize WebM saver using received frame size.
				fmt.Printf("WebmSaver#PushVP8 | InitWriter vWriter:%v aWriter:%v \n", s.videoWriter, s.audioWriter)
				s.InitWriter(width, height)
			}
		}
		if s.videoWriter != nil {
			s.videoTimestamp += sample.Samples
			t := s.videoTimestamp / 90
			if _, err := s.videoWriter.Write(videoKeyframe, int64(t), sample.Data); err != nil {
				panic(err)
			}
		}
	}
}
func (s *WebmSaver) InitWriter(width, height int) {
	w, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		panic(err)
	}

	ws, err := webm.NewSimpleBlockWriter(w,
		[]webm.TrackEntry{
			{
				Name:            "Audio",
				TrackNumber:     1,
				TrackUID:        12345,
				CodecID:         "A_OPUS",
				TrackType:       2,
				DefaultDuration: 20000000,
				Audio: &webm.Audio{
					SamplingFrequency: 48000.0,
					Channels:          2,
				},
			}, {
				Name:            "Video",
				TrackNumber:     2,
				TrackUID:        67890,
				CodecID:         "V_VP8",
				TrackType:       1,
				DefaultDuration: 33333333,
				Video: &webm.Video{
					PixelWidth:  uint64(width),
					PixelHeight: uint64(height),
				},
			},
		})
	if err != nil {
		panic(err)
	}
	fmt.Printf("WebM saver has started with video width=%d, height=%d audioWriter=%v videoWriter=%v \n", width, height, ws[0], ws[1])
	s.audioWriter = ws[0]
	s.videoWriter = ws[1]
}

// WriteSample packetizes and writes to the track
func (s *WebmSaver) WriteSample(sample media.Sample) error {
	packets := s.packetizer.Packetize(sample.Data, sample.Samples)
	//fmt.Printf("WebmSaver#WriteSample | len(packets):%v \n", len(packets))
	for _, p := range packets {
		//fmt.Printf("WebmSaver#WriterSample | packet:%v \n", p)
		err := s.WriteRTP(p)
		if err != nil {
			return err
		}
	}

	return nil
}

// WriteRTP writes RTP packets to the track
func (s *WebmSaver) WriteRTP(p *rtp.Packet) error {
	s.PushVP8(p)

	return nil
}
