package main

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-redis/redis"
	"github.com/unixpickle/essentials"
)

const (
	SubscribeTimeout = time.Second * 10
	ActionTimeout    = time.Minute * 5
)

var ErrNewMaster = errors.New("the master has changed")

// A Session serves up a single environment to remote
// masters.
type Session struct {
	client          *redis.Client
	pubsub          *redis.PubSub
	channelPrefix   string
	envID           string
	masterReadyChan string
}

// NewSession establishes a database connection.
func NewSession(host, channelPrefix string) (*Session, error) {
	envID := fmt.Sprintf("%12x", rand.Intn(0x1000000000000))
	for {
		client := redis.NewClient(&redis.Options{Addr: host})
		if err := client.Ping().Err(); err != nil {
			return nil, err
		}
		masterReadyChan := channelPrefix + ":master-ready"
		ps := client.Subscribe(channelPrefix+":act:"+envID, masterReadyChan)
		_, err := ps.ReceiveTimeout(SubscribeTimeout)
		if err != nil {
			client.Close()
			ps.Close()
			return nil, err
		}
		return &Session{
			client:          client,
			pubsub:          ps,
			channelPrefix:   channelPrefix,
			envID:           envID,
			masterReadyChan: masterReadyChan,
		}, nil
	}
}

// envID returns the environment ID.
func (s *Session) EnvID() string {
	return s.envID
}

// SendState publishes an environment state update.
func (s *Session) SendState(state []byte) error {
	return essentials.AddCtx("SendState",
		s.client.Publish(s.channelPrefix+":state:"+s.envID, state).Err())
}

// ReceiveAct receives an action from a master.
func (s *Session) ReceiveAct() (act [4]bool, err error) {
	defer essentials.AddCtxTo("ReceiveAct", &err)
	deadline := time.Now().Add(ActionTimeout)
	for {
		timeout := deadline.Sub(time.Now())
		if timeout <= 0 {
			return [4]bool{}, errors.New("action timeout exceeded")
		}
		msg, err := s.pubsub.ReceiveTimeout(timeout)
		if err != nil {
			return [4]bool{}, err
		}
		if msg, ok := msg.(*redis.Message); ok {
			if msg.Channel == s.masterReadyChan {
				return [4]bool{}, ErrNewMaster
			}
			if len(msg.Payload) != 4 {
				return [4]bool{}, errors.New("invalid payload size")
			}
			return [4]bool{
				msg.Payload[0] != '0',
				msg.Payload[1] != '0',
				msg.Payload[2] != '0',
				msg.Payload[3] != '0',
			}, nil
		}
	}
}

// SendRawScreen sends screenshot data to any script that
// is recording video.
func (s *Session) SendRawScreen(channel string, data []byte) error {
	return essentials.AddCtx("SendRawScreen", s.client.Publish(channel, data).Err())
}

func (s *Session) Close() error {
	s.pubsub.Close()
	return s.client.Close()
}
