package ibft

import (
	"github.com/0xPolygon/go-ibft/messages/proto"
	"github.com/0xPolygon/polygon-edge/network"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
)

type IBFTMsgHandler func(*proto.Message)

type EdgeGossipTransport struct {
	logger hclog.Logger
	topic  *network.Topic
}

func NewEdgeGossipTransport(logger hclog.Logger, server *network.Server) (*EdgeGossipTransport, error) {
	// Define a new topic
	topic, err := server.NewTopic(ibftProto, &proto.Message{})
	if err != nil {
		return nil, err
	}

	return &EdgeGossipTransport{logger, topic}, nil
}

func (g *EdgeGossipTransport) Start(handleMsg IBFTMsgHandler) error {
	// Subscribe to the newly created topic
	return g.topic.Subscribe(
		func(obj interface{}, _ peer.ID) {
			msg, ok := obj.(*proto.Message)
			if ok {
				handleMsg(msg)

				g.logger.Debug(
					"validator message received",
					"type", msg.Type.String(),
					"height", msg.GetView().Height,
					"round", msg.GetView().Round,
					"addr", types.BytesToAddress(msg.From).String(),
				)
			} else {
				g.logger.Error("invalid type assertion for message request")
			}

		},
	)
}

func (g *EdgeGossipTransport) Multicast(msg *proto.Message) {
	if err := g.topic.Publish(msg); err != nil {
		g.logger.Error("fail to gossip", "err", err)
	}
}
