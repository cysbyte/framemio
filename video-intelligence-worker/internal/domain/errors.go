package domain

import "errors"

var (
	ErrNotFound           = errors.New("record not found")
	ErrPermanent          = errors.New("permanent processing error")
	ErrUnsupportedMedia   = errors.New("unsupported media type")
	ErrInvalidModelOutput = errors.New("invalid multimodal model output")
	ErrInvalidTranscript  = errors.New("invalid transcript")
)

func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrPermanent, err)
}

func IsPermanent(err error) bool { return errors.Is(err, ErrPermanent) }
