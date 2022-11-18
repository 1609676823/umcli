package kwm

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"unlock-music.dev/cli/algo/common"
)

const magicHeader = "yeelion-kuwo-tme"
const keyPreDefined = "MoOtOiTvINGwd2E6n0E1i7L5t2IoOoNk"

type Decoder struct {
	cipher common.StreamDecoder

	rd     io.ReadSeeker
	offset int

	outputExt string
	bitrate   int
}

func (d *Decoder) GetAudioExt() string {
	return "." + d.outputExt
}

func NewDecoder(rd io.ReadSeeker) common.Decoder {
	return &Decoder{rd: rd}
}

func (d *Decoder) Validate() error {
	header := make([]byte, 0x400) // kwm header is fixed to 1024 bytes
	_, err := io.ReadFull(d.rd, header)
	if err != nil {
		return fmt.Errorf("kwm read header: %w", err)
	}

	// check magic header, 0x00 - 0x0F
	if !bytes.Equal([]byte(magicHeader), header[:len(magicHeader)]) {
		return errors.New("kwm magic header not matched")
	}

	d.cipher = newKwmCipher(header[0x18:0x20])                      // Crypto Key, 0x18 - 0x1F
	d.bitrate, d.outputExt = parseBitrateAndType(header[0x30:0x38]) // Bitrate & File Extension, 0x30 - 0x38

	return nil
}

func parseBitrateAndType(header []byte) (int, string) {
	tmp := strings.TrimRight(string(header), "\x00")
	sep := strings.IndexFunc(tmp, func(r rune) bool {
		return !unicode.IsDigit(r)
	})

	bitrate, _ := strconv.Atoi(tmp[:sep]) // just ignore the error
	outputExt := strings.ToLower(tmp[sep:])
	return bitrate, outputExt
}

func (d *Decoder) Read(b []byte) (int, error) {
	n, err := d.rd.Read(b)
	if n > 0 {
		d.cipher.Decrypt(b[:n], d.offset)
		d.offset += n
	}
	return n, err
}

func padOrTruncate(raw string, length int) string {
	lenRaw := len(raw)
	out := raw
	if lenRaw == 0 {
		out = string(make([]byte, length))
	} else if lenRaw > length {
		out = raw[:length]
	} else if lenRaw < length {
		_tmp := make([]byte, 32)
		for i := 0; i < 32; i++ {
			_tmp[i] = raw[i%lenRaw]
		}
		out = string(_tmp)
	}
	return out
}

func init() {
	// Kuwo Mp3/Flac
	common.RegisterDecoder("kwm", false, NewDecoder)
	common.RegisterDecoder("kwm", false, common.NewRawDecoder)
}
