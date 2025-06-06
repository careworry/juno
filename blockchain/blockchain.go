package blockchain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/db"
	"github.com/NethermindEth/juno/encoder"
	"github.com/NethermindEth/juno/feed"
	"github.com/NethermindEth/juno/utils"
	"github.com/ethereum/go-ethereum/common"
)

type L1HeadSubscription struct {
	*feed.Subscription[*core.L1Head]
}

type BlockSignFunc func(blockHash, stateDiffCommitment *felt.Felt) ([]*felt.Felt, error)

//go:generate mockgen -destination=../mocks/mock_blockchain.go -package=mocks github.com/NethermindEth/juno/blockchain Reader
type Reader interface {
	Height() (height uint64, err error)

	Head() (head *core.Block, err error)
	L1Head() (*core.L1Head, error)
	SubscribeL1Head() L1HeadSubscription
	BlockByNumber(number uint64) (block *core.Block, err error)
	BlockByHash(hash *felt.Felt) (block *core.Block, err error)

	HeadsHeader() (header *core.Header, err error)
	BlockHeaderByNumber(number uint64) (header *core.Header, err error)
	BlockHeaderByHash(hash *felt.Felt) (header *core.Header, err error)

	TransactionByHash(hash *felt.Felt) (transaction core.Transaction, err error)
	TransactionByBlockNumberAndIndex(blockNumber, index uint64) (transaction core.Transaction, err error)
	Receipt(hash *felt.Felt) (receipt *core.TransactionReceipt, blockHash *felt.Felt, blockNumber uint64, err error)
	StateUpdateByNumber(number uint64) (update *core.StateUpdate, err error)
	StateUpdateByHash(hash *felt.Felt) (update *core.StateUpdate, err error)
	L1HandlerTxnHash(msgHash *common.Hash) (l1HandlerTxnHash *felt.Felt, err error)

	HeadState() (core.StateReader, StateCloser, error)
	StateAtBlockHash(blockHash *felt.Felt) (core.StateReader, StateCloser, error)
	StateAtBlockNumber(blockNumber uint64) (core.StateReader, StateCloser, error)

	BlockCommitmentsByNumber(blockNumber uint64) (*core.BlockCommitments, error)

	EventFilter(from *felt.Felt, keys [][]felt.Felt) (EventFilterer, error)

	Network() *utils.Network
}

var (
	ErrParentDoesNotMatchHead = errors.New("block's parent hash does not match head block hash")
	SupportedStarknetVersion  = semver.MustParse("0.13.3")
)

func CheckBlockVersion(protocolVersion string) error {
	blockVer, err := core.ParseBlockVersion(protocolVersion)
	if err != nil {
		return err
	}

	// We ignore changes in patch part of the version
	blockVerMM, supportedVerMM := copyWithoutPatch(blockVer), copyWithoutPatch(SupportedStarknetVersion)
	if blockVerMM.GreaterThan(supportedVerMM) {
		return errors.New("unsupported block version")
	}

	return nil
}

func copyWithoutPatch(v *semver.Version) *semver.Version {
	if v == nil {
		return nil
	}

	return semver.New(v.Major(), v.Minor(), 0, v.Prerelease(), v.Metadata())
}

var _ Reader = (*Blockchain)(nil)

// Blockchain is responsible for keeping track of all things related to the Starknet blockchain
type Blockchain struct {
	network        *utils.Network
	database       db.DB
	listener       EventListener
	l1HeadFeed     *feed.Feed[*core.L1Head]
	pendingBlockFn func() *core.Block
}

func New(database db.DB, network *utils.Network) *Blockchain {
	return &Blockchain{
		database:   database,
		network:    network,
		listener:   &SelectiveListener{},
		l1HeadFeed: feed.New[*core.L1Head](),
	}
}

func (b *Blockchain) WithPendingBlockFn(pendingBlockFn func() *core.Block) *Blockchain {
	b.pendingBlockFn = pendingBlockFn
	return b
}

func (b *Blockchain) WithListener(listener EventListener) *Blockchain {
	b.listener = listener
	return b
}

func (b *Blockchain) Network() *utils.Network {
	return b.network
}

// StateCommitment returns the latest block state commitment.
// If blockchain is empty zero felt is returned.
func (b *Blockchain) StateCommitment() (*felt.Felt, error) {
	b.listener.OnRead("StateCommitment")
	var commitment *felt.Felt
	return commitment, b.database.View(func(txn db.Transaction) error {
		var err error
		commitment, err = core.NewState(txn).Root()
		return err
	})
}

// Height returns the latest block height. If blockchain is empty nil is returned.
func (b *Blockchain) Height() (uint64, error) {
	b.listener.OnRead("Height")
	var height uint64
	return height, b.database.View(func(txn db.Transaction) error {
		var err error
		height, err = core.ChainHeight(txn)
		return err
	})
}

func (b *Blockchain) Head() (*core.Block, error) {
	b.listener.OnRead("Head")
	var h *core.Block
	return h, b.database.View(func(txn db.Transaction) error {
		var err error
		h, err = head(txn)
		return err
	})
}

func (b *Blockchain) HeadsHeader() (*core.Header, error) {
	b.listener.OnRead("HeadsHeader")
	var header *core.Header

	return header, b.database.View(func(txn db.Transaction) error {
		var err error
		header, err = headsHeader(txn)
		return err
	})
}

func head(txn db.Transaction) (*core.Block, error) {
	height, err := core.ChainHeight(txn)
	if err != nil {
		return nil, err
	}
	return BlockByNumber(txn, height)
}

func headsHeader(txn db.Transaction) (*core.Header, error) {
	height, err := core.ChainHeight(txn)
	if err != nil {
		return nil, err
	}

	return blockHeaderByNumber(txn, height)
}

func (b *Blockchain) BlockByNumber(number uint64) (*core.Block, error) {
	b.listener.OnRead("BlockByNumber")
	var block *core.Block
	return block, b.database.View(func(txn db.Transaction) error {
		var err error
		block, err = BlockByNumber(txn, number)
		return err
	})
}

func (b *Blockchain) BlockHeaderByNumber(number uint64) (*core.Header, error) {
	b.listener.OnRead("BlockHeaderByNumber")
	var header *core.Header
	return header, b.database.View(func(txn db.Transaction) error {
		var err error
		header, err = blockHeaderByNumber(txn, number)
		return err
	})
}

func (b *Blockchain) BlockByHash(hash *felt.Felt) (*core.Block, error) {
	b.listener.OnRead("BlockByHash")
	var block *core.Block
	return block, b.database.View(func(txn db.Transaction) error {
		var err error
		block, err = blockByHash(txn, hash)
		return err
	})
}

func (b *Blockchain) BlockHeaderByHash(hash *felt.Felt) (*core.Header, error) {
	b.listener.OnRead("BlockHeaderByHash")
	var header *core.Header
	return header, b.database.View(func(txn db.Transaction) error {
		var err error
		header, err = blockHeaderByHash(txn, hash)
		return err
	})
}

func (b *Blockchain) StateUpdateByNumber(number uint64) (*core.StateUpdate, error) {
	b.listener.OnRead("StateUpdateByNumber")
	var update *core.StateUpdate
	return update, b.database.View(func(txn db.Transaction) error {
		var err error
		update, err = stateUpdateByNumber(txn, number)
		return err
	})
}

func (b *Blockchain) StateUpdateByHash(hash *felt.Felt) (*core.StateUpdate, error) {
	b.listener.OnRead("StateUpdateByHash")
	var update *core.StateUpdate
	return update, b.database.View(func(txn db.Transaction) error {
		var err error
		update, err = stateUpdateByHash(txn, hash)
		return err
	})
}

func (b *Blockchain) L1HandlerTxnHash(msgHash *common.Hash) (*felt.Felt, error) {
	b.listener.OnRead("L1HandlerTxnHash")
	var l1HandlerTxnHash *felt.Felt
	return l1HandlerTxnHash, b.database.View(func(txn db.Transaction) error {
		var err error
		l1HandlerTxnHash, err = l1HandlerTxnHashByMsgHash(txn, msgHash)
		return err
	})
}

// TransactionByBlockNumberAndIndex gets the transaction for a given block number and index.
func (b *Blockchain) TransactionByBlockNumberAndIndex(blockNumber, index uint64) (core.Transaction, error) {
	b.listener.OnRead("TransactionByBlockNumberAndIndex")
	var transaction core.Transaction
	return transaction, b.database.View(func(txn db.Transaction) error {
		var err error
		transaction, err = transactionByBlockNumberAndIndex(txn, &txAndReceiptDBKey{blockNumber, index})
		return err
	})
}

// TransactionByHash gets the transaction for a given hash.
func (b *Blockchain) TransactionByHash(hash *felt.Felt) (core.Transaction, error) {
	b.listener.OnRead("TransactionByHash")
	var transaction core.Transaction
	return transaction, b.database.View(func(txn db.Transaction) error {
		var err error
		transaction, err = transactionByHash(txn, hash)
		return err
	})
}

// Receipt gets the transaction receipt for a given transaction hash.
func (b *Blockchain) Receipt(hash *felt.Felt) (*core.TransactionReceipt, *felt.Felt, uint64, error) {
	b.listener.OnRead("Receipt")
	var (
		receipt     *core.TransactionReceipt
		blockHash   *felt.Felt
		blockNumber uint64
	)
	return receipt, blockHash, blockNumber, b.database.View(func(txn db.Transaction) error {
		var err error
		receipt, blockHash, blockNumber, err = receiptByHash(txn, hash)
		return err
	})
}

func (b *Blockchain) SubscribeL1Head() L1HeadSubscription {
	return L1HeadSubscription{b.l1HeadFeed.Subscribe()}
}

func (b *Blockchain) L1Head() (*core.L1Head, error) {
	b.listener.OnRead("L1Head")
	var update *core.L1Head

	return update, b.database.View(func(txn db.Transaction) error {
		var err error
		update, err = l1Head(txn)
		return err
	})
}

func l1Head(txn db.Transaction) (*core.L1Head, error) {
	var update *core.L1Head
	if err := txn.Get(db.L1Height.Key(), func(updateBytes []byte) error {
		return encoder.Unmarshal(updateBytes, &update)
	}); err != nil {
		return nil, err
	}
	return update, nil
}

func (b *Blockchain) SetL1Head(update *core.L1Head) error {
	updateBytes, err := encoder.Marshal(update)
	if err != nil {
		return err
	}

	if err := b.database.Update(func(txn db.Transaction) error {
		return txn.Set(db.L1Height.Key(), updateBytes)
	}); err != nil {
		return err
	}

	b.l1HeadFeed.Send(update)
	return nil
}

// Store takes a block and state update and performs sanity checks before putting in the database.
func (b *Blockchain) Store(block *core.Block, blockCommitments *core.BlockCommitments,
	stateUpdate *core.StateUpdate, newClasses map[felt.Felt]core.Class,
) error {
	return b.database.Update(func(txn db.Transaction) error {
		if err := verifyBlock(txn, block); err != nil {
			return err
		}

		if err := core.NewState(txn).Update(block.Number, stateUpdate, newClasses, false); err != nil {
			return err
		}
		if err := StoreBlockHeader(txn, block.Header); err != nil {
			return err
		}

		for i, tx := range block.Transactions {
			if err := storeTransactionAndReceipt(txn, block.Number, uint64(i), tx,
				block.Receipts[i]); err != nil {
				return err
			}
		}

		if err := storeStateUpdate(txn, block.Number, stateUpdate); err != nil {
			return err
		}

		if err := StoreBlockCommitments(txn, block.Number, blockCommitments); err != nil {
			return err
		}

		if err := StoreL1HandlerMsgHashes(txn, block.Transactions); err != nil {
			return err
		}

		return core.SetChainHeight(txn, block.Number)
	})
}

// VerifyBlock assumes the block has already been sanity-checked.
func (b *Blockchain) VerifyBlock(block *core.Block) error {
	return b.database.View(func(txn db.Transaction) error {
		return verifyBlock(txn, block)
	})
}

func verifyBlock(txn db.Transaction, block *core.Block) error {
	if err := CheckBlockVersion(block.ProtocolVersion); err != nil {
		return err
	}

	expectedBlockNumber := uint64(0)
	expectedParentHash := &felt.Zero

	h, err := headsHeader(txn)
	if err == nil {
		expectedBlockNumber = h.Number + 1
		expectedParentHash = h.Hash
	} else if !errors.Is(err, db.ErrKeyNotFound) {
		return err
	}

	if expectedBlockNumber != block.Number {
		return fmt.Errorf("expected block #%d, got block #%d", expectedBlockNumber, block.Number)
	}
	if !block.ParentHash.Equal(expectedParentHash) {
		return ErrParentDoesNotMatchHead
	}

	return nil
}

func StoreBlockCommitments(txn db.Transaction, blockNumber uint64, commitments *core.BlockCommitments) error {
	numBytes := core.MarshalBlockNumber(blockNumber)

	commitmentBytes, err := encoder.Marshal(commitments)
	if err != nil {
		return err
	}

	return txn.Set(db.BlockCommitments.Key(numBytes), commitmentBytes)
}

func (b *Blockchain) BlockCommitmentsByNumber(blockNumber uint64) (*core.BlockCommitments, error) {
	b.listener.OnRead("BlockCommitmentsByNumber")
	var commitments *core.BlockCommitments
	return commitments, b.database.View(func(txn db.Transaction) error {
		var err error
		commitments, err = blockCommitmentsByNumber(txn, blockNumber)
		return err
	})
}

func blockCommitmentsByNumber(txn db.Transaction, blockNumber uint64) (*core.BlockCommitments, error) {
	numBytes := core.MarshalBlockNumber(blockNumber)

	var commitments *core.BlockCommitments
	if err := txn.Get(db.BlockCommitments.Key(numBytes), func(val []byte) error {
		commitments = new(core.BlockCommitments)
		return encoder.Unmarshal(val, commitments)
	}); err != nil {
		return nil, err
	}
	return commitments, nil
}

// StoreBlockHeader stores the given block in the database.
// The db storage for blocks is maintained by two buckets as follows:
//
// [db.BlockHeaderNumbersByHash](BlockHash) -> (BlockNumber)
// [db.BlockHeadersByNumber](BlockNumber) -> (BlockHeader)
//
// "[]" is the db prefix to represent a bucket
// "()" are additional keys appended to the prefix or multiple values marshalled together
// "->" represents a key value pair.
func StoreBlockHeader(txn db.Transaction, header *core.Header) error {
	numBytes := core.MarshalBlockNumber(header.Number)

	if err := txn.Set(db.BlockHeaderNumbersByHash.Key(header.Hash.Marshal()), numBytes); err != nil {
		return err
	}

	headerBytes, err := encoder.Marshal(header)
	if err != nil {
		return err
	}

	return txn.Set(db.BlockHeadersByNumber.Key(numBytes), headerBytes)
}

// blockHeaderByNumber retrieves a block header from database by its number
func blockHeaderByNumber(txn db.Transaction, number uint64) (*core.Header, error) {
	numBytes := core.MarshalBlockNumber(number)

	var header *core.Header
	if err := txn.Get(db.BlockHeadersByNumber.Key(numBytes), func(val []byte) error {
		header = new(core.Header)
		return encoder.Unmarshal(val, header)
	}); err != nil {
		return nil, err
	}
	return header, nil
}

func blockHeaderByHash(txn db.Transaction, hash *felt.Felt) (*core.Header, error) {
	var header *core.Header
	return header, txn.Get(db.BlockHeaderNumbersByHash.Key(hash.Marshal()), func(val []byte) error {
		var err error
		header, err = blockHeaderByNumber(txn, binary.BigEndian.Uint64(val))
		return err
	})
}

// BlockByNumber retrieves a block from database by its number
func BlockByNumber(txn db.Transaction, number uint64) (*core.Block, error) {
	header, err := blockHeaderByNumber(txn, number)
	if err != nil {
		return nil, err
	}

	block := new(core.Block)
	block.Header = header
	block.Transactions, err = TransactionsByBlockNumber(txn, number)
	if err != nil {
		return nil, err
	}

	block.Receipts, err = receiptsByBlockNumber(txn, number)
	if err != nil {
		return nil, err
	}
	return block, nil
}

func TransactionsByBlockNumber(txn db.Transaction, number uint64) ([]core.Transaction, error) {
	numBytes := core.MarshalBlockNumber(number)
	prefix := db.TransactionsByBlockNumberAndIndex.Key(numBytes)

	iterator, err := txn.NewIterator(prefix, true)
	if err != nil {
		return nil, err
	}

	var txs []core.Transaction
	for iterator.First(); iterator.Valid(); iterator.Next() {
		val, vErr := iterator.Value()
		if vErr != nil {
			return nil, utils.RunAndWrapOnError(iterator.Close, vErr)
		}

		var tx core.Transaction
		if err = encoder.Unmarshal(val, &tx); err != nil {
			return nil, utils.RunAndWrapOnError(iterator.Close, err)
		}

		txs = append(txs, tx)
	}

	if err = iterator.Close(); err != nil {
		return nil, err
	}

	return txs, nil
}

func receiptsByBlockNumber(txn db.Transaction, number uint64) ([]*core.TransactionReceipt, error) {
	numBytes := core.MarshalBlockNumber(number)
	prefix := db.ReceiptsByBlockNumberAndIndex.Key(numBytes)

	iterator, err := txn.NewIterator(prefix, true)
	if err != nil {
		return nil, err
	}

	var receipts []*core.TransactionReceipt

	for iterator.First(); iterator.Valid(); iterator.Next() {
		if !bytes.HasPrefix(iterator.Key(), prefix) {
			break
		}

		val, vErr := iterator.Value()
		if vErr != nil {
			return nil, utils.RunAndWrapOnError(iterator.Close, vErr)
		}

		receipt := new(core.TransactionReceipt)
		if err = encoder.Unmarshal(val, receipt); err != nil {
			return nil, utils.RunAndWrapOnError(iterator.Close, err)
		}

		receipts = append(receipts, receipt)
	}

	if err = iterator.Close(); err != nil {
		return nil, err
	}

	return receipts, nil
}

// blockByHash retrieves a block from database by its hash
func blockByHash(txn db.Transaction, hash *felt.Felt) (*core.Block, error) {
	var block *core.Block
	return block, txn.Get(db.BlockHeaderNumbersByHash.Key(hash.Marshal()), func(val []byte) error {
		var err error
		block, err = BlockByNumber(txn, binary.BigEndian.Uint64(val))
		return err
	})
}

func StoreL1HandlerMsgHashes(dbTxn db.Transaction, blockTxns []core.Transaction) error {
	for _, txn := range blockTxns {
		if l1Handler, ok := (txn).(*core.L1HandlerTransaction); ok {
			err := dbTxn.Set(db.L1HandlerTxnHashByMsgHash.Key(l1Handler.MessageHash()), txn.Hash().Marshal())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func storeStateUpdate(txn db.Transaction, blockNumber uint64, update *core.StateUpdate) error {
	numBytes := core.MarshalBlockNumber(blockNumber)

	updateBytes, err := encoder.Marshal(update)
	if err != nil {
		return err
	}

	return txn.Set(db.StateUpdatesByBlockNumber.Key(numBytes), updateBytes)
}

func stateUpdateByNumber(txn db.Transaction, blockNumber uint64) (*core.StateUpdate, error) {
	numBytes := core.MarshalBlockNumber(blockNumber)

	var update *core.StateUpdate
	if err := txn.Get(db.StateUpdatesByBlockNumber.Key(numBytes), func(val []byte) error {
		update = new(core.StateUpdate)
		return encoder.Unmarshal(val, update)
	}); err != nil {
		return nil, err
	}
	return update, nil
}

func stateUpdateByHash(txn db.Transaction, hash *felt.Felt) (*core.StateUpdate, error) {
	var update *core.StateUpdate
	return update, txn.Get(db.BlockHeaderNumbersByHash.Key(hash.Marshal()), func(val []byte) error {
		var err error
		update, err = stateUpdateByNumber(txn, binary.BigEndian.Uint64(val))
		return err
	})
}

func l1HandlerTxnHashByMsgHash(txn db.Transaction, l1HandlerMsgHash *common.Hash) (*felt.Felt, error) {
	l1HandlerTxnHash := new(felt.Felt)
	return l1HandlerTxnHash, txn.Get(db.L1HandlerTxnHashByMsgHash.Key(l1HandlerMsgHash.Bytes()), func(val []byte) error {
		l1HandlerTxnHash.Unmarshal(val)
		return nil
	})
}

// SanityCheckNewHeight checks integrity of a block and resulting state update
func (b *Blockchain) SanityCheckNewHeight(block *core.Block, stateUpdate *core.StateUpdate,
	newClasses map[felt.Felt]core.Class,
) (*core.BlockCommitments, error) {
	if !block.Hash.Equal(stateUpdate.BlockHash) {
		return nil, errors.New("block hashes do not match")
	}
	if !block.GlobalStateRoot.Equal(stateUpdate.NewRoot) {
		return nil, errors.New("block's GlobalStateRoot does not match state update's NewRoot")
	}

	if err := core.VerifyClassHashes(newClasses); err != nil {
		return nil, err
	}

	return core.VerifyBlockHash(block, b.network, stateUpdate.StateDiff)
}

type txAndReceiptDBKey struct {
	Number uint64
	Index  uint64
}

func (t *txAndReceiptDBKey) MarshalBinary() []byte {
	return binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64([]byte{}, t.Number), t.Index)
}

func (t *txAndReceiptDBKey) UnmarshalBinary(data []byte) error {
	r := bytes.NewReader(data)
	if err := binary.Read(r, binary.BigEndian, &t.Number); err != nil {
		return err
	}
	return binary.Read(r, binary.BigEndian, &t.Index)
}

// storeTransactionAndReceipt stores the given transaction receipt in the database.
// The db storage for transaction and receipts is maintained by three buckets as follows:
//
// [db.TransactionBlockNumbersAndIndicesByHash](TransactionHash) -> (BlockNumber, Index)
// [db.TransactionsByBlockNumberAndIndex](BlockNumber, Index) -> Transaction
// [db.ReceiptsByBlockNumberAndIndex](BlockNumber, Index) -> Receipt
//
// Note: we are using the same transaction hash bucket which keeps track of block number and
// index for both transactions and receipts since transaction and its receipt share the same hash.
// "[]" is the db prefix to represent a bucket
// "()" are additional keys appended to the prefix or multiple values marshalled together
// "->" represents a key value pair.
func storeTransactionAndReceipt(txn db.Transaction, number, i uint64, t core.Transaction, r *core.TransactionReceipt) error {
	bnIndexBytes := (&txAndReceiptDBKey{number, i}).MarshalBinary()

	if err := txn.Set(db.TransactionBlockNumbersAndIndicesByHash.Key((r.TransactionHash).Marshal()),
		bnIndexBytes); err != nil {
		return err
	}

	txnBytes, err := encoder.Marshal(t)
	if err != nil {
		return err
	}
	if err = txn.Set(db.TransactionsByBlockNumberAndIndex.Key(bnIndexBytes), txnBytes); err != nil {
		return err
	}

	rBytes, err := encoder.Marshal(r)
	if err != nil {
		return err
	}
	return txn.Set(db.ReceiptsByBlockNumberAndIndex.Key(bnIndexBytes), rBytes)
}

// transactionBlockNumberAndIndexByHash gets the block number and index for a given transaction hash
func transactionBlockNumberAndIndexByHash(txn db.Transaction, hash *felt.Felt) (*txAndReceiptDBKey, error) {
	var bnIndex *txAndReceiptDBKey
	if err := txn.Get(db.TransactionBlockNumbersAndIndicesByHash.Key(hash.Marshal()), func(val []byte) error {
		bnIndex = new(txAndReceiptDBKey)
		return bnIndex.UnmarshalBinary(val)
	}); err != nil {
		return nil, err
	}
	return bnIndex, nil
}

// transactionByBlockNumberAndIndex gets the transaction for a given block number and index.
func transactionByBlockNumberAndIndex(txn db.Transaction, bnIndex *txAndReceiptDBKey) (core.Transaction, error) {
	var transaction core.Transaction
	err := txn.Get(db.TransactionsByBlockNumberAndIndex.Key(bnIndex.MarshalBinary()), func(val []byte) error {
		return encoder.Unmarshal(val, &transaction)
	})
	return transaction, err
}

// transactionByHash gets the transaction for a given hash.
func transactionByHash(txn db.Transaction, hash *felt.Felt) (core.Transaction, error) {
	bnIndex, err := transactionBlockNumberAndIndexByHash(txn, hash)
	if err != nil {
		return nil, err
	}
	return transactionByBlockNumberAndIndex(txn, bnIndex)
}

// receiptByHash gets the transaction receipt for a given hash.
func receiptByHash(txn db.Transaction, hash *felt.Felt) (*core.TransactionReceipt, *felt.Felt, uint64, error) {
	bnIndex, err := transactionBlockNumberAndIndexByHash(txn, hash)
	if err != nil {
		return nil, nil, 0, err
	}

	receipt, err := receiptByBlockNumberAndIndex(txn, bnIndex)
	if err != nil {
		return nil, nil, 0, err
	}

	header, err := blockHeaderByNumber(txn, bnIndex.Number)
	if err != nil {
		return nil, nil, 0, err
	}

	return receipt, header.Hash, header.Number, nil
}

// receiptByBlockNumberAndIndex gets the transaction receipt for a given block number and index.
func receiptByBlockNumberAndIndex(txn db.Transaction, bnIndex *txAndReceiptDBKey) (*core.TransactionReceipt, error) {
	var r *core.TransactionReceipt
	err := txn.Get(db.ReceiptsByBlockNumberAndIndex.Key(bnIndex.MarshalBinary()), func(val []byte) error {
		return encoder.Unmarshal(val, &r)
	})
	return r, err
}

type StateCloser = func() error

// HeadState returns a StateReader that provides a stable view to the latest state
func (b *Blockchain) HeadState() (core.StateReader, StateCloser, error) {
	b.listener.OnRead("HeadState")
	txn, err := b.database.NewTransaction(false)
	if err != nil {
		return nil, nil, err
	}

	_, err = core.ChainHeight(txn)
	if err != nil {
		return nil, nil, utils.RunAndWrapOnError(txn.Discard, err)
	}

	return core.NewState(txn), txn.Discard, nil
}

// StateAtBlockNumber returns a StateReader that provides a stable view to the state at the given block number
func (b *Blockchain) StateAtBlockNumber(blockNumber uint64) (core.StateReader, StateCloser, error) {
	b.listener.OnRead("StateAtBlockNumber")
	txn, err := b.database.NewTransaction(false)
	if err != nil {
		return nil, nil, err
	}

	_, err = blockHeaderByNumber(txn, blockNumber)
	if err != nil {
		return nil, nil, utils.RunAndWrapOnError(txn.Discard, err)
	}

	return core.NewStateSnapshot(core.NewState(txn), blockNumber), txn.Discard, nil
}

// StateAtBlockHash returns a StateReader that provides a stable view to the state at the given block hash
func (b *Blockchain) StateAtBlockHash(blockHash *felt.Felt) (core.StateReader, StateCloser, error) {
	b.listener.OnRead("StateAtBlockHash")
	if blockHash.IsZero() {
		txn := db.NewMemTransaction()
		emptyState := core.NewState(txn)
		return emptyState, txn.Discard, nil
	}

	txn, err := b.database.NewTransaction(false)
	if err != nil {
		return nil, nil, err
	}

	header, err := blockHeaderByHash(txn, blockHash)
	if err != nil {
		return nil, nil, utils.RunAndWrapOnError(txn.Discard, err)
	}

	return core.NewStateSnapshot(core.NewState(txn), header.Number), txn.Discard, nil
}

// EventFilter returns an EventFilter object that is tied to a snapshot of the blockchain
func (b *Blockchain) EventFilter(from *felt.Felt, keys [][]felt.Felt) (EventFilterer, error) {
	b.listener.OnRead("EventFilter")
	txn, err := b.database.NewTransaction(false)
	if err != nil {
		return nil, err
	}

	latest, err := core.ChainHeight(txn)
	if err != nil {
		return nil, err
	}

	return newEventFilter(txn, from, keys, 0, latest, b.pendingBlockFn), nil
}

// RevertHead reverts the head block
func (b *Blockchain) RevertHead() error {
	return b.database.Update(b.revertHead)
}

func (b *Blockchain) GetReverseStateDiff() (*core.StateDiff, error) {
	var reverseStateDiff *core.StateDiff
	return reverseStateDiff, b.database.View(func(txn db.Transaction) error {
		blockNumber, err := core.ChainHeight(txn)
		if err != nil {
			return err
		}
		stateUpdate, err := stateUpdateByNumber(txn, blockNumber)
		if err != nil {
			return err
		}
		state := core.NewState(txn)
		reverseStateDiff, err = state.GetReverseStateDiff(blockNumber, stateUpdate.StateDiff)
		return err
	})
}

func (b *Blockchain) revertHead(txn db.Transaction) error {
	blockNumber, err := core.ChainHeight(txn)
	if err != nil {
		return err
	}
	numBytes := core.MarshalBlockNumber(blockNumber)

	stateUpdate, err := stateUpdateByNumber(txn, blockNumber)
	if err != nil {
		return err
	}

	state := core.NewState(txn)
	// revert state
	if err = state.Revert(blockNumber, stateUpdate); err != nil {
		return err
	}

	header, err := blockHeaderByNumber(txn, blockNumber)
	if err != nil {
		return err
	}

	genesisBlock := blockNumber == 0

	// remove block header
	for _, key := range [][]byte{
		db.BlockHeadersByNumber.Key(numBytes),
		db.BlockHeaderNumbersByHash.Key(header.Hash.Marshal()),
		db.BlockCommitments.Key(numBytes),
	} {
		if err = txn.Delete(key); err != nil {
			return err
		}
	}

	if err = removeTxsAndReceipts(txn, blockNumber, header.TransactionCount); err != nil {
		return err
	}

	// remove state update
	if err = txn.Delete(db.StateUpdatesByBlockNumber.Key(numBytes)); err != nil {
		return err
	}

	// Revert chain height.
	if genesisBlock {
		return core.DeleteChainHeight(txn)
	}

	return core.SetChainHeight(txn, blockNumber-1)
}

func removeTxsAndReceipts(txn db.Transaction, blockNumber, numTxs uint64) error {
	blockIDAndIndex := txAndReceiptDBKey{
		Number: blockNumber,
	}
	// remove txs and receipts
	for i := range numTxs {
		blockIDAndIndex.Index = i
		reorgedTxn, err := transactionByBlockNumberAndIndex(txn, &blockIDAndIndex)
		if err != nil {
			return err
		}

		keySuffix := blockIDAndIndex.MarshalBinary()
		if err = txn.Delete(db.TransactionsByBlockNumberAndIndex.Key(keySuffix)); err != nil {
			return err
		}
		if err = txn.Delete(db.ReceiptsByBlockNumberAndIndex.Key(keySuffix)); err != nil {
			return err
		}
		if err = txn.Delete(db.TransactionBlockNumbersAndIndicesByHash.Key(reorgedTxn.Hash().Marshal())); err != nil {
			return err
		}
		if l1handler, ok := reorgedTxn.(*core.L1HandlerTransaction); ok {
			if err = txn.Delete(db.L1HandlerTxnHashByMsgHash.Key(l1handler.MessageHash())); err != nil {
				return err
			}
		}
	}

	return nil
}

// Finalise will calculate the state commitment and block hash for the given pending block and append it to the
// blockchain.
func (b *Blockchain) Finalise(
	block *core.Block,
	stateUpdate *core.StateUpdate,
	newClasses map[felt.Felt]core.Class,
	sign BlockSignFunc,
) error {
	return b.database.Update(func(txn db.Transaction) error {
		if err := b.updateStateRoots(txn, block, stateUpdate, newClasses); err != nil {
			return err
		}

		commitments, err := b.calculateBlockHash(block, stateUpdate)
		if err != nil {
			return err
		}

		if err := b.signBlock(block, stateUpdate, sign); err != nil {
			return err
		}

		if err := b.storeBlockData(txn, block, stateUpdate, commitments); err != nil {
			return err
		}

		// Update chain height
		heightBin := core.MarshalBlockNumber(block.Number)
		return txn.Set(db.ChainHeight.Key(), heightBin)
	})
}

// updateStateRoots computes and updates state roots in the block and state update
func (b *Blockchain) updateStateRoots(
	txn db.Transaction,
	block *core.Block,
	stateUpdate *core.StateUpdate,
	newClasses map[felt.Felt]core.Class,
) error {
	state := core.NewState(txn)

	// Get old state root
	oldStateRoot, err := state.Root()
	if err != nil {
		return err
	}
	stateUpdate.OldRoot = oldStateRoot

	// Apply state update
	if err = state.Update(block.Number, stateUpdate, newClasses, true); err != nil {
		return err
	}

	// Get new state root
	newStateRoot, err := state.Root()
	if err != nil {
		return err
	}

	block.GlobalStateRoot = newStateRoot
	stateUpdate.NewRoot = block.GlobalStateRoot

	return nil
}

// calculateBlockHash computes and sets the block hash and commitments
func (b *Blockchain) calculateBlockHash(block *core.Block, stateUpdate *core.StateUpdate) (*core.BlockCommitments, error) {
	blockHash, commitments, err := core.BlockHash(
		block,
		stateUpdate.StateDiff,
		b.network,
		block.SequencerAddress)
	if err != nil {
		return nil, err
	}
	block.Hash = blockHash
	stateUpdate.BlockHash = blockHash
	return commitments, nil
}

// signBlock applies the signature to the block if a signing function is provided
func (b *Blockchain) signBlock(block *core.Block, stateUpdate *core.StateUpdate, sign BlockSignFunc) error {
	if sign == nil {
		return nil
	}

	sig, err := sign(block.Hash, stateUpdate.StateDiff.Commitment())
	if err != nil {
		return err
	}

	block.Signatures = [][]*felt.Felt{sig}

	return nil
}

// storeBlockData persists all block-related data to the database
func (b *Blockchain) storeBlockData(
	txn db.Transaction,
	block *core.Block,
	stateUpdate *core.StateUpdate,
	commitments *core.BlockCommitments,
) error {
	// Store block header
	if err := StoreBlockHeader(txn, block.Header); err != nil {
		return err
	}

	// Store transactions and receipts
	for i, tx := range block.Transactions {
		if err := storeTransactionAndReceipt(txn, block.Number, uint64(i), tx, block.Receipts[i]); err != nil {
			return err
		}
	}

	// Store state update
	if err := storeStateUpdate(txn, block.Number, stateUpdate); err != nil {
		return err
	}

	// Store block commitments
	if err := StoreBlockCommitments(txn, block.Number, commitments); err != nil {
		return err
	}

	// Store L1 handler message hashes
	if err := StoreL1HandlerMsgHashes(txn, block.Transactions); err != nil {
		return err
	}

	return nil
}

func (b *Blockchain) StoreGenesis(diff *core.StateDiff, classes map[felt.Felt]core.Class) error {
	receipts := make([]*core.TransactionReceipt, 0)

	block := &core.Block{
		Header: &core.Header{
			ParentHash:       &felt.Zero,
			Number:           0,
			SequencerAddress: &felt.Zero,
			EventsBloom:      core.EventsBloom(receipts),
			L1GasPriceETH:    &felt.Zero,
			L1GasPriceSTRK:   &felt.Zero,
		},
		Transactions: make([]core.Transaction, 0),
		Receipts:     receipts,
	}
	stateUpdate := &core.StateUpdate{
		OldRoot:   &felt.Zero,
		StateDiff: diff,
	}
	newClasses := classes

	return b.Finalise(block, stateUpdate, newClasses, func(_, _ *felt.Felt) ([]*felt.Felt, error) {
		return nil, nil
	})
}
