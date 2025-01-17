package tests

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/bits-and-blooms/bitset"
	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/prysmaticlabs/prysm/crypto/bls"
	"github.com/stretchr/testify/suite"

	"github.com/bnb-chain/greenfield/e2e/core"
	"github.com/bnb-chain/greenfield/sdk/types"
	storagetestutil "github.com/bnb-chain/greenfield/testutil/storage"
	challengetypes "github.com/bnb-chain/greenfield/x/challenge/types"
	storagetypes "github.com/bnb-chain/greenfield/x/storage/types"
)

type ChallengeTestSuite struct {
	core.BaseSuite
}

func (s *ChallengeTestSuite) SetupSuite() {
	s.BaseSuite.SetupSuite()
}

func (s *ChallengeTestSuite) SetupTest() {
}

func TestChallengeTestSuite(t *testing.T) {
	suite.Run(t, new(ChallengeTestSuite))
}

func (s *ChallengeTestSuite) createObject() (string, string, *core.StorageProvider) {
	var err error
	sp := s.BaseSuite.PickStorageProvider()
	gvg, found := sp.GetFirstGlobalVirtualGroup()
	s.Require().True(found)

	s.T().Log(sp.Info.String())
	s.T().Log(gvg.String())
	// CreateBucket
	user := s.GenAndChargeAccounts(1, 1000000)[0]
	bucketName := "ch" + storagetestutil.GenRandomBucketName()
	msgCreateBucket := storagetypes.NewMsgCreateBucket(
		user.GetAddr(), bucketName, storagetypes.VISIBILITY_TYPE_PRIVATE, sp.OperatorKey.GetAddr(),
		nil, math.MaxUint, nil, 0)
	msgCreateBucket.PrimarySpApproval.GlobalVirtualGroupFamilyId = gvg.FamilyId
	msgCreateBucket.PrimarySpApproval.Sig, err = sp.ApprovalKey.Sign(msgCreateBucket.GetApprovalBytes())
	s.Require().NoError(err)
	s.SendTxBlock(user, msgCreateBucket)
	// HeadBucket
	ctx := context.Background()
	queryHeadBucketRequest := storagetypes.QueryHeadBucketRequest{
		BucketName: bucketName,
	}
	queryHeadBucketResponse, err := s.Client.HeadBucket(ctx, &queryHeadBucketRequest)
	s.Require().NoError(err)
	s.Require().Equal(queryHeadBucketResponse.BucketInfo.BucketName, bucketName)

	// CreateObject
	objectName := storagetestutil.GenRandomObjectName()
	// create test buffer
	var buffer bytes.Buffer
	line := `1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,1234567890,
	1234567890,1234567890,1234567890,123`
	// Create 1MiB content where each line contains 1024 characters.
	for i := 0; i < 1024; i++ {
		buffer.WriteString(fmt.Sprintf("[%05d] %s\n", i, line))
	}
	payloadSize := buffer.Len()
	checksum := sdk.Keccak256(buffer.Bytes())
	expectChecksum := [][]byte{checksum, checksum, checksum, checksum, checksum, checksum, checksum}
	contextType := "text/event-stream"
	msgCreateObject := storagetypes.NewMsgCreateObject(user.GetAddr(), bucketName, objectName, uint64(payloadSize), storagetypes.VISIBILITY_TYPE_PRIVATE, expectChecksum, contextType, storagetypes.REDUNDANCY_EC_TYPE, math.MaxUint, nil)
	msgCreateObject.PrimarySpApproval.Sig, err = sp.ApprovalKey.Sign(msgCreateObject.GetApprovalBytes())
	s.Require().NoError(err)
	s.SendTxBlock(user, msgCreateObject)

	// HeadObject
	queryHeadObjectRequest := storagetypes.QueryHeadObjectRequest{
		BucketName: bucketName,
		ObjectName: objectName,
	}
	queryHeadObjectResponse, err := s.Client.HeadObject(ctx, &queryHeadObjectRequest)
	s.Require().NoError(err)
	s.Require().Equal(queryHeadObjectResponse.ObjectInfo.ObjectName, objectName)
	s.Require().Equal(queryHeadObjectResponse.ObjectInfo.BucketName, bucketName)

	// SealObject
	gvgId := gvg.Id
	msgSealObject := storagetypes.NewMsgSealObject(sp.SealKey.GetAddr(), bucketName, objectName, gvg.Id, nil)

	secondarySigs := make([][]byte, 0)
	secondarySPBlsPubKeys := make([]bls.PublicKey, 0)
	blsSignHash := storagetypes.NewSecondarySpSealObjectSignDoc(s.GetChainID(), gvgId, queryHeadObjectResponse.ObjectInfo.Id, storagetypes.GenerateHash(queryHeadObjectResponse.ObjectInfo.Checksums[:])).GetBlsSignHash()
	// every secondary sp signs the checksums
	for _, gvgID := range gvg.SecondarySpIds {
		sig, err := core.BlsSignAndVerify(s.StorageProviders[gvgID], blsSignHash)
		s.Require().NoError(err)
		secondarySigs = append(secondarySigs, sig)
		pk, err := bls.PublicKeyFromBytes(s.StorageProviders[gvgID].BlsKey.PubKey().Bytes())
		s.Require().NoError(err)
		secondarySPBlsPubKeys = append(secondarySPBlsPubKeys, pk)
	}
	aggBlsSig, err := core.BlsAggregateAndVerify(secondarySPBlsPubKeys, blsSignHash, secondarySigs)
	s.Require().NoError(err)
	msgSealObject.SecondarySpBlsAggSignatures = aggBlsSig
	s.SendTxBlock(sp.SealKey, msgSealObject)

	queryHeadObjectResponse, err = s.Client.HeadObject(ctx, &queryHeadObjectRequest)
	s.Require().NoError(err)
	s.Require().Equal(queryHeadObjectResponse.ObjectInfo.ObjectName, objectName)
	s.Require().Equal(queryHeadObjectResponse.ObjectInfo.BucketName, bucketName)
	s.Require().Equal(queryHeadObjectResponse.ObjectInfo.ObjectStatus, storagetypes.OBJECT_STATUS_SEALED)

	return bucketName, objectName, sp
}

func (s *ChallengeTestSuite) TestSubmit() {
	user := s.GenAndChargeAccounts(1, 1000000)[0]

	bucketName, objectName, primarySp := s.createObject()
	msgSubmit := challengetypes.NewMsgSubmit(user.GetAddr(), primarySp.OperatorKey.GetAddr(), bucketName, objectName, true, 1000)
	txRes := s.SendTxBlock(user, msgSubmit)
	event := filterChallengeEventFromTx(txRes) // secondary sps are faked with primary sp, redundancy check is meaningless here
	s.Require().GreaterOrEqual(event.ChallengeId, uint64(0))
	s.Require().NotEqual(event.SegmentIndex, uint32(100))
	s.Require().Equal(event.SpOperatorAddress, primarySp.OperatorKey.GetAddr().String())

	bucketName, objectName, _ = s.createObject()

	msgSubmit = challengetypes.NewMsgSubmit(user.GetAddr(), primarySp.OperatorKey.GetAddr(), bucketName, objectName, false, 0)
	txRes = s.SendTxBlock(user, msgSubmit)
	event = filterChallengeEventFromTx(txRes)
	s.Require().GreaterOrEqual(event.ChallengeId, uint64(0))
	s.Require().Equal(event.SegmentIndex, uint32(0))
}

func (s *ChallengeTestSuite) calculateValidatorBitSet(height int64, blsKey string) *bitset.BitSet {
	valBitSet := bitset.New(256)

	page := 1
	size := 10
	valRes, err := s.TmClient.TmClient.Validators(context.Background(), &height, &page, &size)
	if err != nil {
		panic(err)
	}

	for idx, val := range valRes.Validators {
		if strings.EqualFold(blsKey, hex.EncodeToString(val.BlsKey[:])) {
			valBitSet.Set(uint(idx))
		}
	}

	return valBitSet
}

func (s *ChallengeTestSuite) TestNormalAttest() {
	user := s.GenAndChargeAccounts(1, 1000000)[0]

	bucketName, objectName, primarySp := s.createObject()
	msgSubmit := challengetypes.NewMsgSubmit(user.GetAddr(), primarySp.OperatorKey.GetAddr(), bucketName, objectName, true, 1000)
	txRes := s.SendTxBlock(user, msgSubmit)
	event := filterChallengeEventFromTx(txRes)

	statusRes, err := s.TmClient.TmClient.Status(context.Background())
	s.Require().NoError(err)
	height := statusRes.SyncInfo.LatestBlockHeight

	valBitset := s.calculateValidatorBitSet(height, s.ValidatorBLS.PubKey().String())

	msgAttest := challengetypes.NewMsgAttest(s.Challenger.GetAddr(), event.ChallengeId, event.ObjectId, primarySp.OperatorKey.GetAddr().String(),
		challengetypes.CHALLENGE_SUCCEED, user.GetAddr().String(), valBitset.Bytes(), nil)
	toSign := msgAttest.GetBlsSignBytes(s.Config.ChainId)

	voteAggSignature, err := s.ValidatorBLS.Sign(toSign[:])
	if err != nil {
		panic(err)
	}
	msgAttest.VoteAggSignature = voteAggSignature

	// wait to its turn
	for {
		queryRes, err := s.Client.ChallengeQueryClient.InturnAttestationSubmitter(context.Background(), &challengetypes.QueryInturnAttestationSubmitterRequest{})
		s.Require().NoError(err)

		s.T().Logf("current submitter %s, interval: %d - %d", queryRes.BlsPubKey,
			queryRes.SubmitInterval.Start, queryRes.SubmitInterval.End)

		if queryRes.BlsPubKey == hex.EncodeToString(s.ValidatorBLS.PubKey().Bytes()) {
			break
		}
	}

	// submit attest
	txRes = s.SendTxBlock(s.Challenger, msgAttest)
	s.Require().True(txRes.Code == 0)

	queryRes, err := s.Client.ChallengeQueryClient.LatestAttestedChallenges(context.Background(), &challengetypes.QueryLatestAttestedChallengesRequest{})
	s.Require().NoError(err)
	found := false
	result := challengetypes.CHALLENGE_FAILED
	for _, challenge := range queryRes.Challenges {
		if challenge.Id == event.ChallengeId {
			found = true
			result = challenge.Result
			break
		}
	}
	s.Require().True(found)
	s.Require().True(result == challengetypes.CHALLENGE_SUCCEED)
}

func (s *ChallengeTestSuite) TestHeartbeatAttest() {
	for i := 0; i < 3; i++ {
		s.createObject()
	}

	heartbeatInterval := uint64(100)

	var event challengetypes.EventStartChallenge
	found := false
	height := int64(0)
	for {
		statusRes, err := s.TmClient.TmClient.Status(context.Background())
		s.Require().NoError(err)
		height = statusRes.SyncInfo.LatestBlockHeight

		time.Sleep(20 * time.Millisecond)
		blockRes, err := s.TmClient.TmClient.BlockResults(context.Background(), &height)
		s.Require().NoError(err)
		events := filterChallengeEventFromBlock(blockRes)

		for _, e := range events {
			if e.ChallengeId%heartbeatInterval == 0 {
				event = e
				found = true
				break
			}
		}
		if found == true {
			break
		}

		if len(events) > 0 {
			s.T().Logf("current challenge id: %d", events[len(events)-1].ChallengeId)
		}
		time.Sleep(200 * time.Millisecond)
	}

	valBitset := s.calculateValidatorBitSet(height, s.ValidatorBLS.PubKey().String())

	msgAttest := challengetypes.NewMsgAttest(s.Challenger.GetAddr(), event.ChallengeId, event.ObjectId,
		event.SpOperatorAddress, challengetypes.CHALLENGE_FAILED, "", valBitset.Bytes(), nil)
	toSign := msgAttest.GetBlsSignBytes(s.Config.ChainId)

	voteAggSignature, err := s.ValidatorBLS.Sign(toSign[:])
	if err != nil {
		panic(err)
	}
	msgAttest.VoteAggSignature = voteAggSignature

	// wait to its turn
	for {
		queryRes, err := s.Client.ChallengeQueryClient.InturnAttestationSubmitter(context.Background(), &challengetypes.QueryInturnAttestationSubmitterRequest{})
		s.Require().NoError(err)

		s.T().Logf("current submitter %s, interval: %d - %d", queryRes.BlsPubKey,
			queryRes.SubmitInterval.Start, queryRes.SubmitInterval.End)

		if queryRes.BlsPubKey == hex.EncodeToString(s.ValidatorBLS.PubKey().Bytes()) {
			break
		}
	}

	// submit attest
	txRes := s.SendTxBlock(s.Challenger, msgAttest)
	s.Require().True(txRes.Code == 0)

	queryRes, err := s.Client.ChallengeQueryClient.LatestAttestedChallenges(context.Background(), &challengetypes.QueryLatestAttestedChallengesRequest{})
	s.Require().NoError(err)
	found = false
	result := challengetypes.CHALLENGE_SUCCEED
	for _, challenge := range queryRes.Challenges {
		if challenge.Id == event.ChallengeId {
			found = true
			result = challenge.Result
			break
		}
	}
	s.Require().True(found)
	s.Require().True(result == challengetypes.CHALLENGE_FAILED)
}

func (s *ChallengeTestSuite) TestFailedAttest_ChallengeExpired() {
	user := s.GenAndChargeAccounts(1, 1000000)[0]

	bucketName, objectName, primarySp := s.createObject()
	msgSubmit := challengetypes.NewMsgSubmit(user.GetAddr(), primarySp.OperatorKey.GetAddr(), bucketName, objectName, true, 1000)
	txRes := s.SendTxBlock(user, msgSubmit)
	event := filterChallengeEventFromTx(txRes)

	statusRes, err := s.TmClient.TmClient.Status(context.Background())
	s.Require().NoError(err)

	expiredHeight := event.ExpiredHeight
	for {
		time.Sleep(200 * time.Millisecond)
		statusRes, err := s.TmClient.TmClient.Status(context.Background())
		s.Require().NoError(err)
		height := statusRes.SyncInfo.LatestBlockHeight

		s.T().Logf("current height: %d, expired height: %d", height, expiredHeight)

		if uint64(height) > expiredHeight {
			break
		}
	}

	height := statusRes.SyncInfo.LatestBlockHeight
	valBitset := s.calculateValidatorBitSet(height, s.ValidatorBLS.PubKey().String())

	msgAttest := challengetypes.NewMsgAttest(user.GetAddr(), event.ChallengeId, event.ObjectId, primarySp.OperatorKey.GetAddr().String(),
		challengetypes.CHALLENGE_SUCCEED, user.GetAddr().String(), valBitset.Bytes(), nil)
	toSign := msgAttest.GetBlsSignBytes(s.Config.ChainId)

	voteAggSignature, err := s.ValidatorBLS.Sign(toSign[:])
	if err != nil {
		panic(err)
	}
	msgAttest.VoteAggSignature = voteAggSignature

	s.SendTxBlockWithExpectErrorString(msgAttest, user, challengetypes.ErrInvalidChallengeId.Error())
}

func (s *ChallengeTestSuite) TestEndBlock() {
	for i := 0; i < 3; i++ {
		s.createObject()
	}

	statusRes, err := s.TmClient.TmClient.Status(context.Background())
	s.Require().NoError(err)
	height := statusRes.SyncInfo.LatestBlockHeight

	blockRes, err := s.TmClient.TmClient.BlockResults(context.Background(), &height)
	s.Require().NoError(err)
	events := filterChallengeEventFromBlock(blockRes)
	s.Require().True(len(events) > 0)
}

func filterChallengeEventFromBlock(blockRes *ctypes.ResultBlockResults) []challengetypes.EventStartChallenge {
	challengeEvents := make([]challengetypes.EventStartChallenge, 0)

	for _, event := range blockRes.EndBlockEvents {
		if event.Type == "greenfield.challenge.EventStartChallenge" {

			challengeIdStr, objectIdStr, redundancyIndexStr, segmentIndexStr, spOpAddress := "", "", "", "", ""
			for _, attr := range event.Attributes {
				if string(attr.Key) == "challenge_id" {
					challengeIdStr = strings.Trim(string(attr.Value), `"`)
				} else if string(attr.Key) == "object_id" {
					objectIdStr = strings.Trim(string(attr.Value), `"`)
				} else if string(attr.Key) == "redundancy_index" {
					redundancyIndexStr = strings.Trim(string(attr.Value), `"`)
				} else if string(attr.Key) == "segment_index" {
					segmentIndexStr = strings.Trim(string(attr.Value), `"`)
				} else if string(attr.Key) == "sp_operator_address" {
					spOpAddress = strings.Trim(string(attr.Value), `"`)
				}
			}
			challengeId, _ := strconv.ParseInt(challengeIdStr, 10, 64)
			objectId := sdkmath.NewUintFromString(objectIdStr)
			redundancyIndex, _ := strconv.ParseInt(redundancyIndexStr, 10, 32)
			segmentIndex, _ := strconv.ParseInt(segmentIndexStr, 10, 32)
			challengeEvents = append(challengeEvents, challengetypes.EventStartChallenge{
				ChallengeId:       uint64(challengeId),
				ObjectId:          objectId,
				SegmentIndex:      uint32(segmentIndex),
				SpOperatorAddress: spOpAddress,
				RedundancyIndex:   int32(redundancyIndex),
			})
		}
	}
	return challengeEvents
}

func filterChallengeEventFromTx(txRes *sdk.TxResponse) challengetypes.EventStartChallenge {
	challengeIdStr, objectIdStr, redundancyIndexStr, segmentIndexStr, spOpAddress, expiredHeightStr := "", "", "", "", "", ""
	for _, event := range txRes.Logs[0].Events {
		if event.Type == "greenfield.challenge.EventStartChallenge" {
			for _, attr := range event.Attributes {
				if attr.Key == "challenge_id" {
					challengeIdStr = strings.Trim(attr.Value, `"`)
				} else if attr.Key == "object_id" {
					objectIdStr = strings.Trim(attr.Value, `"`)
				} else if attr.Key == "redundancy_index" {
					redundancyIndexStr = strings.Trim(attr.Value, `"`)
				} else if attr.Key == "segment_index" {
					segmentIndexStr = strings.Trim(attr.Value, `"`)
				} else if attr.Key == "sp_operator_address" {
					spOpAddress = strings.Trim(attr.Value, `"`)
				} else if attr.Key == "expired_height" {
					expiredHeightStr = strings.Trim(attr.Value, `"`)
				}
			}
		}
	}
	challengeId, _ := strconv.ParseInt(challengeIdStr, 10, 64)
	objectId := sdkmath.NewUintFromString(objectIdStr)
	redundancyIndex, _ := strconv.ParseInt(redundancyIndexStr, 10, 32)
	segmentIndex, _ := strconv.ParseInt(segmentIndexStr, 10, 32)
	expiredHeight, _ := strconv.ParseInt(expiredHeightStr, 10, 64)
	return challengetypes.EventStartChallenge{
		ChallengeId:       uint64(challengeId),
		ObjectId:          objectId,
		SegmentIndex:      uint32(segmentIndex),
		SpOperatorAddress: spOpAddress,
		RedundancyIndex:   int32(redundancyIndex),
		ExpiredHeight:     uint64(expiredHeight),
	}
}

func (s *ChallengeTestSuite) TestUpdateChallengerParams() {
	// 1. create proposal
	govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	queryParamsResp, err := s.Client.ChallengeQueryClient.Params(context.Background(), &challengetypes.QueryParamsRequest{})
	s.Require().NoError(err)

	updatedParams := queryParamsResp.Params
	updatedParams.HeartbeatInterval = 1800
	msgUpdateParams := &challengetypes.MsgUpdateParams{
		Authority: govAddr,
		Params:    updatedParams,
	}

	proposal, err := v1.NewMsgSubmitProposal([]sdk.Msg{msgUpdateParams}, sdk.NewCoins(sdk.NewCoin("BNB", sdk.NewInt(1000000000000000000))),
		s.Validator.GetAddr().String(), "", "update Challenger params", "Test update Challenger params")
	s.Require().NoError(err)
	txBroadCastResp, err := s.SendTxBlockWithoutCheck(proposal, s.Validator)
	s.Require().NoError(err)
	s.T().Log("create proposal tx hash: ", txBroadCastResp.TxResponse.TxHash)

	// get proposal id
	proposalID := 0
	txResp, err := s.WaitForTx(txBroadCastResp.TxResponse.TxHash)
	s.Require().NoError(err)
	if txResp.Code == 0 && txResp.Height > 0 {
		for _, event := range txResp.Events {
			if event.Type == "submit_proposal" {
				proposalID, err = strconv.Atoi(event.GetAttributes()[0].Value)
				s.Require().NoError(err)
			}
		}
	}

	// 2. vote
	if proposalID == 0 {
		s.T().Errorf("proposalID is 0")
		return
	}
	s.T().Log("proposalID: ", proposalID)
	mode := tx.BroadcastMode_BROADCAST_MODE_SYNC
	txOpt := &types.TxOption{
		Mode:      &mode,
		Memo:      "",
		FeeAmount: sdk.NewCoins(sdk.NewCoin("BNB", sdk.NewInt(1000000000000000000))),
	}
	voteBroadCastResp, err := s.SendTxBlockWithoutCheckWithTxOpt(v1.NewMsgVote(s.Validator.GetAddr(), uint64(proposalID), v1.OptionYes, ""),
		s.Validator, txOpt)
	s.Require().NoError(err)
	voteResp, err := s.WaitForTx(voteBroadCastResp.TxResponse.TxHash)
	s.Require().NoError(err)
	s.T().Log("vote tx hash: ", voteResp.TxHash)
	if voteResp.Code > 0 {
		s.T().Errorf("voteTxResp.Code > 0")
		return
	}

	// 3. query proposal until it is end voting period
CheckProposalStatus:
	for {
		queryProposalResp, err := s.Client.Proposal(context.Background(), &v1.QueryProposalRequest{ProposalId: uint64(proposalID)})
		s.Require().NoError(err)
		if queryProposalResp.Proposal.Status != v1.StatusVotingPeriod {
			switch queryProposalResp.Proposal.Status {
			case v1.StatusDepositPeriod:
				s.T().Errorf("proposal deposit period")
				return
			case v1.StatusRejected:
				s.T().Errorf("proposal rejected")
				return
			case v1.StatusPassed:
				s.T().Logf("proposal passed")
				break CheckProposalStatus
			case v1.StatusFailed:
				s.T().Errorf("proposal failed, reason %s", queryProposalResp.Proposal.FailedReason)
				return
			}
		}
		time.Sleep(1 * time.Second)
	}

	// 4. check params updated
	err = s.WaitForNextBlock()
	s.Require().NoError(err)

	updatedQueryParamsResp, err := s.Client.ChallengeQueryClient.Params(context.Background(), &challengetypes.QueryParamsRequest{})
	s.Require().NoError(err)
	if reflect.DeepEqual(updatedQueryParamsResp.Params, updatedParams) {
		s.T().Logf("update params success")
	} else {
		s.T().Errorf("update params failed")
	}
}
