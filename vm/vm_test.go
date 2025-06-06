package vm

import (
	"os"
	"testing"

	"github.com/NethermindEth/juno/clients/feeder"
	"github.com/NethermindEth/juno/core"
	"github.com/NethermindEth/juno/core/felt"
	"github.com/NethermindEth/juno/db/pebble"
	"github.com/NethermindEth/juno/rpc/rpccore"
	adaptfeeder "github.com/NethermindEth/juno/starknetdata/feeder"
	"github.com/NethermindEth/juno/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallDeprecatedCairo(t *testing.T) {
	testDB := pebble.NewMemTest(t)
	txn, err := testDB.NewTransaction(true)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, txn.Discard())
	})
	client := feeder.NewTestClient(t, &utils.Mainnet)
	gw := adaptfeeder.New(client)

	contractAddr := utils.HexToFelt(t, "0xDEADBEEF")
	// https://voyager.online/class/0x03297a93c52357144b7da71296d7e8231c3e0959f0a1d37222204f2f7712010e
	classHash := utils.HexToFelt(t, "0x3297a93c52357144b7da71296d7e8231c3e0959f0a1d37222204f2f7712010e")
	simpleClass, err := gw.Class(t.Context(), classHash)
	require.NoError(t, err)

	testState := core.NewState(txn)
	require.NoError(t, testState.Update(0, &core.StateUpdate{
		OldRoot: &felt.Zero,
		NewRoot: utils.HexToFelt(t, "0x3d452fbb3c3a32fe85b1a3fbbcdec316d5fc940cefc028ee808ad25a15991c8"),
		StateDiff: &core.StateDiff{
			DeployedContracts: map[felt.Felt]*felt.Felt{
				*contractAddr: classHash,
			},
		},
	}, map[felt.Felt]core.Class{
		*classHash: simpleClass,
	}, false))

	entryPoint := utils.HexToFelt(t, "0x39e11d48192e4333233c7eb19d10ad67c362bb28580c604d67884c85da39695")

	ret, err := New(false, nil).Call(&CallInfo{
		ContractAddress: contractAddr,
		ClassHash:       classHash,
		Selector:        entryPoint,
	}, &BlockInfo{Header: &core.Header{}}, testState, &utils.Mainnet, 1_000_000, simpleClass.SierraVersion(), false, false)
	require.NoError(t, err)
	assert.Equal(t, []*felt.Felt{&felt.Zero}, ret.Result)

	require.NoError(t, testState.Update(1, &core.StateUpdate{
		OldRoot: utils.HexToFelt(t, "0x3d452fbb3c3a32fe85b1a3fbbcdec316d5fc940cefc028ee808ad25a15991c8"),
		NewRoot: utils.HexToFelt(t, "0x4a948783e8786ba9d8edaf42de972213bd2deb1b50c49e36647f1fef844890f"),
		StateDiff: &core.StateDiff{
			StorageDiffs: map[felt.Felt]map[felt.Felt]*felt.Felt{
				*contractAddr: {
					*utils.HexToFelt(t, "0x206f38f7e4f15e87567361213c28f235cccdaa1d7fd34c9db1dfe9489c6a091"): new(felt.Felt).SetUint64(1337),
				},
			},
		},
	}, nil, false))

	ret, err = New(false, nil).Call(&CallInfo{
		ContractAddress: contractAddr,
		ClassHash:       classHash,
		Selector:        entryPoint,
	}, &BlockInfo{Header: &core.Header{Number: 1}}, testState, &utils.Mainnet, 1_000_000, simpleClass.SierraVersion(), false, false)
	require.NoError(t, err)
	assert.Equal(t, []*felt.Felt{new(felt.Felt).SetUint64(1337)}, ret.Result)
}

func TestCallDeprecatedCairoMaxSteps(t *testing.T) {
	testDB := pebble.NewMemTest(t)
	txn, err := testDB.NewTransaction(true)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, txn.Discard())
	})
	client := feeder.NewTestClient(t, &utils.Mainnet)
	gw := adaptfeeder.New(client)

	contractAddr := utils.HexToFelt(t, "0xDEADBEEF")
	// https://voyager.online/class/0x03297a93c52357144b7da71296d7e8231c3e0959f0a1d37222204f2f7712010e
	classHash := utils.HexToFelt(t, "0x3297a93c52357144b7da71296d7e8231c3e0959f0a1d37222204f2f7712010e")
	simpleClass, err := gw.Class(t.Context(), classHash)
	require.NoError(t, err)

	testState := core.NewState(txn)
	require.NoError(t, testState.Update(0, &core.StateUpdate{
		OldRoot: &felt.Zero,
		NewRoot: utils.HexToFelt(t, "0x3d452fbb3c3a32fe85b1a3fbbcdec316d5fc940cefc028ee808ad25a15991c8"),
		StateDiff: &core.StateDiff{
			DeployedContracts: map[felt.Felt]*felt.Felt{
				*contractAddr: classHash,
			},
		},
	}, map[felt.Felt]core.Class{
		*classHash: simpleClass,
	}, false))

	entryPoint := utils.HexToFelt(t, "0x39e11d48192e4333233c7eb19d10ad67c362bb28580c604d67884c85da39695")

	_, err = New(false, nil).Call(&CallInfo{
		ContractAddress: contractAddr,
		ClassHash:       classHash,
		Selector:        entryPoint,
	}, &BlockInfo{Header: &core.Header{}}, testState, &utils.Mainnet, 0, simpleClass.SierraVersion(), false, false)
	assert.ErrorContains(t, err, "RunResources has no remaining steps")
}

func TestCallCairo(t *testing.T) {
	testDB := pebble.NewMemTest(t)
	txn, err := testDB.NewTransaction(true)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, txn.Discard())
	})
	client := feeder.NewTestClient(t, &utils.Goerli)
	gw := adaptfeeder.New(client)

	contractAddr := utils.HexToFelt(t, "0xDEADBEEF")
	// https://goerli.voyager.online/class/0x01338d85d3e579f6944ba06c005238d145920afeb32f94e3a1e234d21e1e9292
	classHash := utils.HexToFelt(t, "0x1338d85d3e579f6944ba06c005238d145920afeb32f94e3a1e234d21e1e9292")
	simpleClass, err := gw.Class(t.Context(), classHash)
	require.NoError(t, err)

	testState := core.NewState(txn)
	require.NoError(t, testState.Update(0, &core.StateUpdate{
		OldRoot: &felt.Zero,
		NewRoot: utils.HexToFelt(t, "0x2650cef46c190ec6bb7dc21a5a36781132e7c883b27175e625031149d4f1a84"),
		StateDiff: &core.StateDiff{
			DeployedContracts: map[felt.Felt]*felt.Felt{
				*contractAddr: classHash,
			},
		},
	}, map[felt.Felt]core.Class{
		*classHash: simpleClass,
	}, false))

	logLevel := utils.NewLogLevel(utils.ERROR)
	log, err := utils.NewZapLogger(logLevel, false)
	require.NoError(t, err)

	// test_storage_read
	entryPoint := utils.HexToFelt(t, "0x5df99ae77df976b4f0e5cf28c7dcfe09bd6e81aab787b19ac0c08e03d928cf")
	storageLocation := utils.HexToFelt(t, "0x44")
	ret, err := New(false, log).Call(&CallInfo{
		ContractAddress: contractAddr,
		Selector:        entryPoint,
		Calldata: []felt.Felt{
			*storageLocation,
		},
	}, &BlockInfo{Header: &core.Header{}}, testState, &utils.Goerli, 1_000_000, simpleClass.SierraVersion(), false, false)
	require.NoError(t, err)
	assert.Equal(t, []*felt.Felt{&felt.Zero}, ret.Result)

	require.NoError(t, testState.Update(1, &core.StateUpdate{
		OldRoot: utils.HexToFelt(t, "0x2650cef46c190ec6bb7dc21a5a36781132e7c883b27175e625031149d4f1a84"),
		NewRoot: utils.HexToFelt(t, "0x7a9da0a7471a8d5118d3eefb8c26a6acbe204eb1eaa934606f4757a595fe552"),
		StateDiff: &core.StateDiff{
			StorageDiffs: map[felt.Felt]map[felt.Felt]*felt.Felt{
				*contractAddr: {
					*storageLocation: new(felt.Felt).SetUint64(37),
				},
			},
		},
	}, nil, false))

	ret, err = New(false, log).Call(&CallInfo{
		ContractAddress: contractAddr,
		Selector:        entryPoint,
		Calldata: []felt.Felt{
			*storageLocation,
		},
	}, &BlockInfo{Header: &core.Header{Number: 1}}, testState, &utils.Goerli, 1_000_000, simpleClass.SierraVersion(), false, false)
	require.NoError(t, err)
	assert.Equal(t, []*felt.Felt{new(felt.Felt).SetUint64(37)}, ret.Result)
}

func TestCallInfoErrorHandling(t *testing.T) {
	testDB := pebble.NewMemTest(t)
	txn, err := testDB.NewTransaction(true)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, txn.Discard())
	})
	client := feeder.NewTestClient(t, &utils.Sepolia)
	gw := adaptfeeder.New(client)

	contractAddr := utils.HexToFelt(t, "0x123")
	classHash := utils.HexToFelt(t, "0x5f18f9cdc05da87f04e8e7685bd346fc029f977167d5b1b2b59f69a7dacbfc8")
	simpleClass, err := gw.Class(t.Context(), classHash)
	require.NoError(t, err)

	testState := core.NewState(txn)
	require.NoError(t, testState.Update(0, &core.StateUpdate{
		OldRoot: &felt.Zero,
		NewRoot: utils.HexToFelt(t, "0xa6258de574e5540253c4a52742137d58b9e8ad8f584115bee46d9d18255c42"),
		StateDiff: &core.StateDiff{
			DeployedContracts: map[felt.Felt]*felt.Felt{
				*contractAddr: classHash,
			},
		},
	}, map[felt.Felt]core.Class{
		*classHash: simpleClass,
	}, false))

	logLevel := utils.NewLogLevel(utils.ERROR)
	log, err := utils.NewZapLogger(logLevel, false)
	require.NoError(t, err)

	callInfo := &CallInfo{
		ClassHash:       classHash,
		ContractAddress: contractAddr,
		Selector:        utils.HexToFelt(t, "0x123"), // doesn't exist
		Calldata:        []felt.Felt{},
	}

	// Starknet version <0.13.4 should return an error
	ret, err := New(false, log).Call(callInfo, &BlockInfo{Header: &core.Header{
		ProtocolVersion: "0.13.0",
	}}, testState, &utils.Sepolia, 1_000_000, simpleClass.SierraVersion(), false, false)
	require.Equal(t, CallResult{}, ret)
	require.ErrorContains(t, err, "not found in contract")

	// Starknet version 0.13.4 should return an "error" in the CallInfo
	ret, err = New(false, log).Call(callInfo, &BlockInfo{Header: &core.Header{
		ProtocolVersion: "0.13.4",
	}}, testState, &utils.Sepolia, 1_000_000, simpleClass.SierraVersion(), false, false)
	require.True(t, ret.ExecutionFailed)
	require.Equal(t, len(ret.Result), 1)
	require.Equal(t, ret.Result[0].String(), rpccore.EntrypointNotFoundFelt)
	require.Nil(t, err)
}

func TestExecute(t *testing.T) {
	network := utils.Goerli2

	testDB := pebble.NewMemTest(t)
	txn, err := testDB.NewTransaction(false)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, txn.Discard())
	})

	state := core.NewState(txn)

	t.Run("empty transaction list", func(t *testing.T) {
		_, err := New(false, nil).Execute([]core.Transaction{}, []core.Class{}, []*felt.Felt{}, &BlockInfo{
			Header: &core.Header{
				Timestamp:        1666877926,
				SequencerAddress: utils.HexToFelt(t, "0x46a89ae102987331d369645031b49c27738ed096f2789c24449966da4c6de6b"),
				L1GasPriceETH:    &felt.Zero,
				L1GasPriceSTRK:   &felt.Zero,
			},
		}, state,
			&network, false, false, false, false, false)
		require.NoError(t, err)
	})
	t.Run("zero data", func(t *testing.T) {
		_, err := New(false, nil).Execute(nil, nil, []*felt.Felt{}, &BlockInfo{
			Header: &core.Header{
				SequencerAddress: &felt.Zero,
				L1GasPriceETH:    &felt.Zero,
				L1GasPriceSTRK:   &felt.Zero,
			},
		}, state, &network, false, false, false, false, false)
		require.NoError(t, err)
	})
}

func TestSetVersionedConstants(t *testing.T) {
	t.Run("valid custom versioned constants file (1 overwrite)", func(t *testing.T) {
		require.NoError(t, SetVersionedConstants("testdata/versioned_constants/custom_versioned_constants.json"))
	})

	t.Run("valid custom versioned constants file (multiple overwrites)", func(t *testing.T) {
		require.NoError(t, SetVersionedConstants("testdata/versioned_constants/custom_versioned_constants_multiple.json"))
	})

	t.Run("not valid json", func(t *testing.T) {
		fd, err := os.CreateTemp(t.TempDir(), "versioned_constants_test*")
		require.NoError(t, err)
		defer os.Remove(fd.Name())

		err = SetVersionedConstants(fd.Name())
		assert.ErrorContains(t, err, "Failed to parse JSON")
	})

	t.Run("not exists", func(t *testing.T) {
		assert.ErrorContains(t, SetVersionedConstants("not_exists.json"), "no such file or directory")
	})
}
