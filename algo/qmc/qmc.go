package qmc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"unlock-music.dev/cli/algo/common"
)

type Decoder struct {
	raw      io.ReadSeeker
	audio    io.Reader
	offset   int
	audioLen int

	cipher common.StreamDecoder

	decodedKey    []byte
	rawMetaExtra1 int
	rawMetaExtra2 int
}

// Read implements io.Reader, offer the decrypted audio data.
// Validate should call before Read to check if the file is valid.
func (d *Decoder) Read(p []byte) (int, error) {
	n, err := d.audio.Read(p)
	if n > 0 {
		d.cipher.Decrypt(p[:n], d.offset)
		d.offset += n
	}
	return n, err
}

func NewDecoder(r io.ReadSeeker) common.Decoder {
	return &Decoder{raw: r}
}

func (d *Decoder) Validate() error {
	// search & derive key
	err := d.searchKey()
	if err != nil {
		return err
	}

	// check cipher type and init decode cipher
	if len(d.decodedKey) > 300 {
		d.cipher, err = newRC4Cipher(d.decodedKey)
		if err != nil {
			return err
		}
	} else if len(d.decodedKey) != 0 {
		d.cipher, err = newMapCipher(d.decodedKey)
		if err != nil {
			return err
		}
	} else {
		d.cipher = newStaticCipher()
	}

	// test with first 16 bytes
	if err := d.validateDecode(); err != nil {
		return err
	}

	// reset position, limit to audio, prepare for Read
	if _, err := d.raw.Seek(0, io.SeekStart); err != nil {
		return err
	}
	d.audio = io.LimitReader(d.raw, int64(d.audioLen))

	return nil
}

func (d *Decoder) validateDecode() error {
	_, err := d.raw.Seek(0, io.SeekStart)
	if err != nil {
		return fmt.Errorf("qmc seek to start: %w", err)
	}

	buf := make([]byte, 16)
	if _, err := io.ReadFull(d.raw, buf); err != nil {
		return fmt.Errorf("qmc read header: %w", err)
	}

	d.cipher.Decrypt(buf, 0)
	_, ok := common.SniffAll(buf)
	if !ok {
		return errors.New("qmc: detect file type failed")
	}
	return nil
}

func (d *Decoder) searchKey() error {
	fileSizeM4, err := d.raw.Seek(-4, io.SeekEnd)
	if err != nil {
		return err
	}
	buf, err := io.ReadAll(io.LimitReader(d.raw, 4))
	if err != nil {
		return err
	}
	if string(buf) == "QTag" {
		return d.readRawMetaQTag()
	} else if string(buf) == "STag" {
		return errors.New("qmc: file with 'STag' suffix doesn't contains media key")
	} else {
		size := binary.LittleEndian.Uint32(buf)
		if size <= 0xFFFF && size != 0 { // assume size is key len
			return d.readRawKey(int64(size))
		}
		// try to use default static cipher
		d.audioLen = int(fileSizeM4 + 4)
		return nil
	}
}

func (d *Decoder) readRawKey(rawKeyLen int64) error {
	audioLen, err := d.raw.Seek(-(4 + rawKeyLen), io.SeekEnd)
	if err != nil {
		return err
	}
	d.audioLen = int(audioLen)

	rawKeyData, err := io.ReadAll(io.LimitReader(d.raw, rawKeyLen))
	if err != nil {
		return err
	}

	// clean suffix NULs
	rawKeyData = bytes.TrimRight(rawKeyData, "\x00")

	d.decodedKey, err = deriveKey(rawKeyData)
	if err != nil {
		return err
	}

	return nil
}

func (d *Decoder) readRawMetaQTag() error {
	// get raw meta data len
	if _, err := d.raw.Seek(-8, io.SeekEnd); err != nil {
		return err
	}
	buf, err := io.ReadAll(io.LimitReader(d.raw, 4))
	if err != nil {
		return err
	}
	rawMetaLen := int64(binary.BigEndian.Uint32(buf))

	// read raw meta data
	audioLen, err := d.raw.Seek(-(8 + rawMetaLen), io.SeekEnd)
	if err != nil {
		return err
	}
	d.audioLen = int(audioLen)
	rawMetaData, err := io.ReadAll(io.LimitReader(d.raw, rawMetaLen))
	if err != nil {
		return err
	}

	items := strings.Split(string(rawMetaData), ",")
	if len(items) != 3 {
		return errors.New("invalid raw meta data")
	}

	d.decodedKey, err = deriveKey([]byte(items[0]))
	if err != nil {
		return err
	}

	d.rawMetaExtra1, err = strconv.Atoi(items[1])
	if err != nil {
		return err
	}
	d.rawMetaExtra2, err = strconv.Atoi(items[2])
	if err != nil {
		return err
	}

	return nil
}

//goland:noinspection SpellCheckingInspection
func init() {
	supportedExts := []string{
		"qmc0", "qmc3", //QQ Music MP3
		"qmc2", "qmc4", "qmc6", "qmc8", //QQ Music M4A
		"qmcflac", //QQ Music FLAC
		"qmcogg",  //QQ Music OGG

		"tkm", //QQ Music Accompaniment M4A

		"bkcmp3", "bkcm4a", "bkcflac", "bkcwav", "bkcape", "bkcogg", "bkcwma", //Moo Music

		"666c6163", //QQ Music Weiyun Flac
		"6d7033",   //QQ Music Weiyun Mp3
		"6f6767",   //QQ Music Weiyun Ogg
		"6d3461",   //QQ Music Weiyun M4a
		"776176",   //QQ Music Weiyun Wav

		"mgg", "mgg1", "mggl", //QQ Music New Ogg
		"mflac", "mflac0", //QQ Music New Flac
	}
	for _, ext := range supportedExts {
		common.RegisterDecoder(ext, false, NewDecoder)
	}
}
