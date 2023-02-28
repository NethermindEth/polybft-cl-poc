package ibft

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/0xPolygon/go-ibft/core"
	"github.com/0xPolygon/go-ibft/messages/proto"
	"github.com/0xPolygon/polygon-edge/network"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/0xPolygon/polygon-edge/validators"
	"github.com/hashicorp/go-hclog"
	"github.com/nethermindeth/polybft-cl-poc/ibft/signer"
)

const (
	DefaultEpochSize = 100000
	IbftKeyName      = "validator.key"
	KeyEpochSize     = "epochSize"

	ibftProto = "/ibft/0.2"

	// consensusMetrics is a prefix used for consensus-related metrics
	consensusMetrics = "consensus"
)

const (
	EpochSize  = 30
	QuorumSize = 5
	BlockTime  = 5 * time.Second
)

var (
	ErrInvalidHookParam             = errors.New("invalid IBFT hook param passed in")
	ErrProposerSealByNonValidator   = errors.New("proposer seal by non-validator")
	ErrInvalidMixHash               = errors.New("invalid mixhash")
	ErrInvalidSha3Uncles            = errors.New("invalid sha3 uncles")
	ErrWrongDifficulty              = errors.New("wrong difficulty")
	ErrParentCommittedSealsNotFound = errors.New("parent committed seals not found")
)

// BackendIBFT represents the IBFT consensus mechanism object
type BackendIBFT struct {
	logger hclog.Logger // Reference to the logging

	consensus *core.IBFT
	transport *EdgeGossipTransport

	signer     signer.Signer         // Signer at current sequence
	validators validators.Validators // signer at current sequence

	wg      sync.WaitGroup
	closeCh chan struct{} // Channel for closing
}

// New creates a new IBFT consensus mechanism object
func New(logger hclog.Logger, server *network.Server) (*BackendIBFT, error) {
	logger = logger.Named("ibft")
	transport, err := NewEdgeGossipTransport(logger, server)
	if err != nil {
		return nil, err
	}

	p := &BackendIBFT{
		// References
		logger:    logger,
		transport: transport,

		// Channels
		closeCh: make(chan struct{}),
	}

	p.consensus = core.NewIBFT(logger.Named("Consensus"), p, p.transport)

	types.HeaderHash = func(h *types.Header) types.Hash {
		hash, err := p.signer.CalculateHeaderHash(h)
		if err != nil {
			return types.ZeroHash
		}

		return hash
	}

	return p, nil
}

// Start starts the IBFT consensus
func (i *BackendIBFT) Start(chainHead uint64) error {
	go func() {
		for {
			if i.validators.Includes(i.signer.Address()) {
				i.consensus.RunSequence(context.Background(), chainHead+1)
			}
		}
	}()

	i.transport.Start(func(msg *proto.Message) {
		if i.validators.Includes(i.signer.Address()) {
			i.consensus.AddMessage(msg)
		}
	})

	return nil
}

// verifyHeaderImpl verifies fields including Extra
// for the past or being proposed header
func (i *BackendIBFT) verifyHeaderImpl(
	header *types.Header,
	headerSigner signer.Signer,
	validators validators.Validators,
	shouldVerifyParentCommittedSeals bool,
) error {
	if header.MixHash != signer.IstanbulDigest {
		return ErrInvalidMixHash
	}

	if header.Sha3Uncles != types.EmptyUncleHash {
		return ErrInvalidSha3Uncles
	}

	// difficulty has to match number
	if header.Difficulty != header.Number {
		return ErrWrongDifficulty
	}

	// ensure the extra data is correctly formatted
	if _, err := headerSigner.GetIBFTExtra(header); err != nil {
		return err
	}

	// verify the ProposerSeal
	if err := verifyProposerSeal(
		header,
		headerSigner,
		validators,
	); err != nil {
		return err
	}

	return nil
}

// VerifyHeader wrapper for verifying headers
func (i *BackendIBFT) VerifyHeader(header *types.Header) error {
	// verify all the header fields
	if err := i.verifyHeaderImpl(
		header,
		i.signer,
		i.validators,
		false,
	); err != nil {
		return err
	}

	extra, err := i.signer.GetIBFTExtra(header)
	if err != nil {
		return err
	}

	hashForCommittedSeal, err := i.calculateProposalHash(
		i.signer,
		header,
		extra.RoundNumber,
	)
	if err != nil {
		return err
	}

	// verify the Committed Seals
	// CommittedSeals exists only in the finalized header
	if err := i.signer.VerifyCommittedSeals(
		hashForCommittedSeal,
		extra.CommittedSeals,
		i.validators,
		i.quorumSize(header.Number)(i.validators),
	); err != nil {
		return err
	}

	return nil
}

// quorumSize returns a callback that when executed on a Validators computes
// number of votes required to reach quorum based on the size of the set.
// The blockNumber argument indicates which formula was used to calculate the result (see PRs #513, #549)
func (i *BackendIBFT) quorumSize(blockNumber uint64) QuorumImplementation {
	return func(vals validators.Validators) int {
		return int(vals.Len()*2/3) + 1
	}
}

// Close closes the IBFT consensus mechanism
func (i *BackendIBFT) Close() {
	close(i.closeCh)
	i.wg.Wait()
}

// verifyProposerSeal verifies ProposerSeal in IBFT Extra of header
// and make sure signer belongs to validators
func verifyProposerSeal(
	header *types.Header,
	signer signer.Signer,
	validators validators.Validators,
) error {
	proposer, err := signer.EcrecoverFromHeader(header)
	if err != nil {
		return err
	}

	if !validators.Includes(proposer) {
		return ErrProposerSealByNonValidator
	}

	return nil
}
