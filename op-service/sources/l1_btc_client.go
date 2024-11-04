package sources

import (
	"context"
	"fmt"

	"github.com/babylonlabs-io/finality-gadget/btcclient"
	"github.com/babylonlabs-io/finality-gadget/log"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/ethereum-optimism/optimism/op-node/rollup/derive"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum-optimism/optimism/op-service/sources/caching"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// interface
var _ derive.L1Fetcher = (*L1BTCClient)(nil)

type L1BTCClient struct {
	*btcclient.BitcoinClient
	// cache L1BlockRef by hash
	// common.Hash -> eth.L1BlockRef
	l1BlockRefsCache *caching.LRUCache[common.Hash, eth.L1BlockRef]
}

func NewL1BTCClient(cfg *btcclient.BTCConfig, metrics caching.Metrics, config *L1ClientConfig) (*L1BTCClient, error) {
	zaplog, err := log.NewRootLogger("console", true)
	if err != nil {
		return nil, err
	}
	btcClient, err := btcclient.NewBitcoinClient(cfg, zaplog)
	if err != nil {
		return nil, err
	}

	return &L1BTCClient{
		BitcoinClient:    btcClient,
		l1BlockRefsCache: caching.NewLRUCache[common.Hash, eth.L1BlockRef](metrics, "blockrefs", config.L1BlockRefsCacheSize),
	}, nil
}

func (s *L1BTCClient) FetchReceipts(ctx context.Context, blockHash common.Hash) (eth.BlockInfo, types.Receipts, error) {
	info, txs, err := s.InfoAndTxsByHash(ctx, blockHash)
	if err != nil {
		return nil, nil, fmt.Errorf("querying block: %w", err)
	}
	receipts := make(types.Receipts, len(txs))
	for i, tx := range txs {
		receipts[i] = &types.Receipt{
			Status:            types.ReceiptStatusSuccessful,
			CumulativeGasUsed: 0,
			Logs:              []*types.Log{},
			TxHash:            tx.Hash(),
		}
	}

	return info, receipts, nil
}

func (s *L1BTCClient) InfoAndTxsByHash(ctx context.Context, hash common.Hash) (eth.BlockInfo, types.Transactions, error) {
	chainHash := ConvCommonToChainHash(hash)

	block, err := s.GetBlock(chainHash)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch block by hash %s: %w", hash.String(), err)
	}

	transactions := make(types.Transactions, len(block.Transactions))

	var blockInfo eth.BlockInfo
	return blockInfo, transactions, nil
}

func (s *L1BTCClient) InfoByHash(ctx context.Context, hash common.Hash) (eth.BlockInfo, error) {
	return nil, nil
}

// L1BlockRefByLabel returns the [eth.L1BlockRef] for the given block label.
// Notice, we cannot cache a block reference by label because labels are not guaranteed to be unique.
func (s *L1BTCClient) L1BlockRefByLabel(ctx context.Context, label eth.BlockLabel) (eth.L1BlockRef, error) {
	return eth.L1BlockRef{}, fmt.Errorf("not implemented")

	// info, err := s.InfoByLabel(ctx, label)
	// if err != nil {
	// 	// Both geth and erigon like to serve non-standard errors for the safe and finalized heads, correct that.
	// 	// This happens when the chain just started and nothing is marked as safe/finalized yet.
	// 	if strings.Contains(err.Error(), "block not found") || strings.Contains(err.Error(), "Unknown block") {
	// 		err = ethereum.NotFound
	// 	}
	// 	return eth.L1BlockRef{}, fmt.Errorf("failed to fetch head header: %w", err)
	// }
	// ref := eth.L1BlockRef{
	// Hash:       ConvChainHash(*hash),
	// Number:     num,
	// ParentHash: ConvChainHash(header.PrevBlock),
	// Time:       uint64(header.Timestamp.Unix()),
	// }
	// s.l1BlockRefsCache.Add(ref.Hash, ref)
	// return ref, nil
}

// L1BlockRefByNumber returns an [eth.L1BlockRef] for the given block number.
// Notice, we cannot cache a block reference by number because L1 re-orgs can invalidate the cached block reference.
func (s *L1BTCClient) L1BlockRefByNumber(ctx context.Context, num uint64) (eth.L1BlockRef, error) {
	hash, err := s.GetBlockHashByHeight(num)
	if err != nil {
		return eth.L1BlockRef{}, fmt.Errorf("failed to fetch header by num %d: %w", num, err)
	}
	header, err := s.GetBlockHeaderByHash(hash)
	if err != nil {
		return eth.L1BlockRef{}, fmt.Errorf("failed to fetch header by num %d: %w", num, err)
	}
	ref := eth.L1BlockRef{
		Hash:       ConvChainHashToCommon(*hash),
		Number:     num,
		ParentHash: ConvChainHashToCommon(header.PrevBlock),
		Time:       uint64(header.Timestamp.Unix()),
	}
	s.l1BlockRefsCache.Add(ref.Hash, ref)
	return ref, nil
}

// L1BlockRefByHash returns the [eth.L1BlockRef] for the given block hash.
// We cache the block reference by hash as it is safe to assume collision will not occur.
func (s *L1BTCClient) L1BlockRefByHash(ctx context.Context, hash common.Hash) (eth.L1BlockRef, error) {
	if v, ok := s.l1BlockRefsCache.Get(hash); ok {
		return v, nil
	}
	header, err := s.GetBlockHeaderByHash(ConvCommonToChainHash(hash))
	if err != nil {
		return eth.L1BlockRef{}, fmt.Errorf("failed to fetch header by hash %s: %w", header.BlockHash().String(), err)
	}

	number, err := s.GetBlockHeightByTimestamp(uint64(header.Timestamp.Unix()))
	if err != nil {
		return eth.L1BlockRef{}, fmt.Errorf("failed to fetch header by hash %s: %w", header.BlockHash().String(), err)
	}

	ref := eth.L1BlockRef{
		Hash:       ConvChainHashToCommon(header.BlockHash()),
		Number:     number,
		ParentHash: ConvChainHashToCommon(header.PrevBlock),
		Time:       uint64(header.Timestamp.Unix()),
	}
	s.l1BlockRefsCache.Add(ref.Hash, ref)
	return ref, nil
}

func ConvChainHashToCommon(chainhash chainhash.Hash) (commonHash common.Hash) {
	commonHash = common.Hash{}
	commonHash.SetBytes(chainhash.CloneBytes())
	return commonHash
}

func ConvCommonToChainHash(commonHash common.Hash) (chainHash *chainhash.Hash) {
	chainHash = &chainhash.Hash{}
	chainHash.SetBytes(commonHash.Bytes())
	return chainHash
}
