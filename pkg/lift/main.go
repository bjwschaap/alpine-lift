package lift

import (
	log "github.com/sirupsen/logrus"
)

type Lift struct {
	DataURL string
}

func New(dataURL string) (*Lift, error) {
	return &Lift{
		DataURL: dataURL,
	}, nil
}

func (l *Lift) Start() error {
	log.Info("Lift starting...")
	return nil
}
