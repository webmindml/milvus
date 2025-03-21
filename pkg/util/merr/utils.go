// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package merr

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/milvus-io/milvus-proto/go-api/v2/commonpb"
	"github.com/milvus-io/milvus-proto/go-api/v2/milvuspb"
	"github.com/milvus-io/milvus/pkg/util/paramtable"
)

// Code returns the error code of the given error,
// WARN: DO NOT use this for now
func Code(err error) int32 {
	if err == nil {
		return 0
	}

	cause := errors.Cause(err)
	switch cause := cause.(type) {
	case milvusError:
		return cause.code()

	default:
		if errors.Is(cause, context.Canceled) {
			return CanceledCode
		} else if errors.Is(cause, context.DeadlineExceeded) {
			return TimeoutCode
		} else {
			return errUnexpected.code()
		}
	}
}

func IsRetryableErr(err error) bool {
	if err, ok := err.(milvusError); ok {
		return err.retriable
	}

	return false
}

func IsCanceledOrTimeout(err error) bool {
	return errors.IsAny(err, context.Canceled, context.DeadlineExceeded)
}

// Status returns a status according to the given err,
// returns Success status if err is nil
func Status(err error) *commonpb.Status {
	if err == nil {
		return &commonpb.Status{}
	}

	code := Code(err)
	return &commonpb.Status{
		Code:   code,
		Reason: previousLastError(err).Error(),
		// Deprecated, for compatibility
		ErrorCode: oldCode(code),
		Retriable: IsRetryableErr(err),
		Detail:    err.Error(),
	}
}

func previousLastError(err error) error {
	lastErr := err
	for {
		nextErr := errors.Unwrap(err)
		if nextErr == nil {
			break
		}
		lastErr = err
		err = nextErr
	}
	return lastErr
}

func CheckRPCCall(resp any, err error) error {
	if err != nil {
		return err
	}
	if resp == nil {
		return errUnexpected
	}
	switch resp := resp.(type) {
	case interface{ GetStatus() *commonpb.Status }:
		return Error(resp.GetStatus())
	case *commonpb.Status:
		return Error(resp)
	}
	return nil
}

func Success(reason ...string) *commonpb.Status {
	status := Status(nil)
	// NOLINT
	status.Reason = strings.Join(reason, " ")
	return status
}

// Deprecated
func StatusWithErrorCode(err error, code commonpb.ErrorCode) *commonpb.Status {
	if err == nil {
		return &commonpb.Status{}
	}

	return &commonpb.Status{
		Code:      Code(err),
		Reason:    err.Error(),
		ErrorCode: code,
	}
}

func oldCode(code int32) commonpb.ErrorCode {
	switch code {
	case ErrServiceNotReady.code():
		return commonpb.ErrorCode_NotReadyServe

	case ErrCollectionNotFound.code():
		return commonpb.ErrorCode_CollectionNotExists

	case ErrParameterInvalid.code():
		return commonpb.ErrorCode_IllegalArgument

	case ErrNodeNotMatch.code():
		return commonpb.ErrorCode_NodeIDNotMatch

	case ErrCollectionNotFound.code(), ErrPartitionNotFound.code(), ErrReplicaNotFound.code():
		return commonpb.ErrorCode_MetaFailed

	case ErrReplicaNotAvailable.code(), ErrChannelNotAvailable.code(), ErrNodeNotAvailable.code():
		return commonpb.ErrorCode_NoReplicaAvailable

	case ErrServiceMemoryLimitExceeded.code():
		return commonpb.ErrorCode_InsufficientMemoryToLoad

	case ErrServiceRateLimit.code():
		return commonpb.ErrorCode_RateLimit

	case ErrServiceForceDeny.code():
		return commonpb.ErrorCode_ForceDeny

	case ErrIndexNotFound.code():
		return commonpb.ErrorCode_IndexNotExist

	case ErrSegmentNotFound.code():
		return commonpb.ErrorCode_SegmentNotFound

	case ErrChannelLack.code():
		return commonpb.ErrorCode_MetaFailed

	default:
		return commonpb.ErrorCode_UnexpectedError
	}
}

func OldCodeToMerr(code commonpb.ErrorCode) error {
	switch code {
	case commonpb.ErrorCode_NotReadyServe:
		return ErrServiceNotReady

	case commonpb.ErrorCode_CollectionNotExists:
		return ErrCollectionNotFound

	case commonpb.ErrorCode_IllegalArgument:
		return ErrParameterInvalid

	case commonpb.ErrorCode_NodeIDNotMatch:
		return ErrNodeNotMatch

	case commonpb.ErrorCode_InsufficientMemoryToLoad, commonpb.ErrorCode_MemoryQuotaExhausted:
		return ErrServiceMemoryLimitExceeded

	case commonpb.ErrorCode_DiskQuotaExhausted:
		return ErrServiceDiskLimitExceeded

	case commonpb.ErrorCode_RateLimit:
		return ErrServiceRateLimit

	case commonpb.ErrorCode_ForceDeny:
		return ErrServiceForceDeny

	case commonpb.ErrorCode_IndexNotExist:
		return ErrIndexNotFound

	case commonpb.ErrorCode_SegmentNotFound:
		return ErrSegmentNotFound

	case commonpb.ErrorCode_MetaFailed:
		return ErrChannelNotFound

	default:
		return errUnexpected
	}
}

func Ok(status *commonpb.Status) bool {
	return status.GetErrorCode() == commonpb.ErrorCode_Success && status.GetCode() == 0
}

// Error returns a error according to the given status,
// returns nil if the status is a success status
func Error(status *commonpb.Status) error {
	if Ok(status) {
		return nil
	}

	// use code first
	code := status.GetCode()
	if code == 0 {
		return newMilvusErrorWithDetail(status.GetReason(), status.GetDetail(), Code(OldCodeToMerr(status.GetErrorCode())), false)
	}
	return newMilvusErrorWithDetail(status.GetReason(), status.GetDetail(), code, status.GetRetriable())
}

// CheckHealthy checks whether the state is healthy,
// returns nil if healthy,
// otherwise returns ErrServiceNotReady wrapped with current state
func CheckHealthy(state commonpb.StateCode) error {
	if state != commonpb.StateCode_Healthy {
		return WrapErrServiceNotReady(paramtable.GetRole(), paramtable.GetNodeID(), state.String())
	}

	return nil
}

// CheckHealthyStandby checks whether the state is healthy or standby,
// returns nil if healthy or standby
// otherwise returns ErrServiceNotReady wrapped with current state
// this method only used in GetMetrics
func CheckHealthyStandby(state commonpb.StateCode) error {
	if state != commonpb.StateCode_Healthy && state != commonpb.StateCode_StandBy {
		return WrapErrServiceNotReady(paramtable.GetRole(), paramtable.GetNodeID(), state.String())
	}

	return nil
}

func IsHealthy(stateCode commonpb.StateCode) error {
	if stateCode == commonpb.StateCode_Healthy {
		return nil
	}
	return CheckHealthy(stateCode)
}

func IsHealthyOrStopping(stateCode commonpb.StateCode) error {
	if stateCode == commonpb.StateCode_Healthy || stateCode == commonpb.StateCode_Stopping {
		return nil
	}
	return CheckHealthy(stateCode)
}

func AnalyzeState(role string, nodeID int64, state *milvuspb.ComponentStates) error {
	if err := Error(state.GetStatus()); err != nil {
		return errors.Wrapf(err, "%s=%d not healthy", role, nodeID)
	} else if state := state.GetState().GetStateCode(); state != commonpb.StateCode_Healthy {
		return WrapErrServiceNotReady(role, nodeID, state.String())
	}

	return nil
}

func CheckTargetID(msg *commonpb.MsgBase) error {
	if msg.GetTargetID() != paramtable.GetNodeID() {
		return WrapErrNodeNotMatch(paramtable.GetNodeID(), msg.GetTargetID())
	}

	return nil
}

// Service related
func WrapErrServiceNotReady(role string, sessionID int64, state string, msg ...string) error {
	err := wrapFieldsWithDesc(ErrServiceNotReady,
		state,
		value(role, sessionID),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrServiceUnavailable(reason string, msg ...string) error {
	err := wrapFieldsWithDesc(ErrServiceUnavailable, reason)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrServiceMemoryLimitExceeded(predict, limit float32, msg ...string) error {
	err := wrapFields(ErrServiceMemoryLimitExceeded,
		value("predict", predict),
		value("limit", limit),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrServiceRequestLimitExceeded(limit int32, msg ...string) error {
	err := wrapFields(ErrServiceRequestLimitExceeded,
		value("limit", limit),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrServiceInternal(reason string, msg ...string) error {
	err := wrapFieldsWithDesc(ErrServiceInternal, reason)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrServiceCrossClusterRouting(expectedCluster, actualCluster string, msg ...string) error {
	err := wrapFields(ErrServiceCrossClusterRouting,
		value("expectedCluster", expectedCluster),
		value("actualCluster", actualCluster),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrServiceDiskLimitExceeded(predict, limit float32, msg ...string) error {
	err := wrapFields(ErrServiceDiskLimitExceeded,
		value("predict", predict),
		value("limit", limit),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrServiceRateLimit(rate float64) error {
	return wrapFields(ErrServiceRateLimit, value("rate", rate))
}

func WrapErrServiceForceDeny(op string, reason error, method string) error {
	return wrapFieldsWithDesc(ErrServiceForceDeny,
		reason.Error(),
		value("op", op),
		value("req", method),
	)
}

func WrapErrServiceUnimplemented(grpcErr error) error {
	return wrapFieldsWithDesc(ErrServiceUnimplemented, grpcErr.Error())
}

// database related
func WrapErrDatabaseNotFound(database any, msg ...string) error {
	err := wrapFields(ErrDatabaseNotFound, value("database", database))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrDatabaseNumLimitExceeded(limit int, msg ...string) error {
	err := wrapFields(ErrDatabaseNumLimitExceeded, value("limit", limit))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrDatabaseNameInvalid(database any, msg ...string) error {
	err := wrapFields(ErrDatabaseInvalidName, value("database", database))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Collection related
func WrapErrCollectionNotFound(collection any, msg ...string) error {
	err := wrapFields(ErrCollectionNotFound, value("collection", collection))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrCollectionNotFoundWithDB(db any, collection any, msg ...string) error {
	err := wrapFields(ErrCollectionNotFound,
		value("database", db),
		value("collection", collection),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrCollectionNotLoaded(collection any, msg ...string) error {
	err := wrapFields(ErrCollectionNotLoaded, value("collection", collection))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrCollectionNumLimitExceeded(limit int, msg ...string) error {
	err := wrapFields(ErrCollectionNumLimitExceeded, value("limit", limit))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrCollectionNotFullyLoaded(collection any, msg ...string) error {
	err := wrapFields(ErrCollectionNotFullyLoaded, value("collection", collection))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrAliasNotFound(db any, alias any, msg ...string) error {
	err := wrapFields(ErrAliasNotFound,
		value("database", db),
		value("alias", alias),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrAliasCollectionNameConflict(db any, alias any, msg ...string) error {
	err := wrapFields(ErrAliasCollectionNameConfilct,
		value("database", db),
		value("alias", alias),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrAliasAlreadyExist(db any, alias any, msg ...string) error {
	err := wrapFields(ErrAliasAlreadyExist,
		value("database", db),
		value("alias", alias),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Partition related
func WrapErrPartitionNotFound(partition any, msg ...string) error {
	err := wrapFields(ErrPartitionNotFound, value("partition", partition))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrPartitionNotLoaded(partition any, msg ...string) error {
	err := wrapFields(ErrPartitionNotLoaded, value("partition", partition))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrPartitionNotFullyLoaded(partition any, msg ...string) error {
	err := wrapFields(ErrPartitionNotFullyLoaded, value("partition", partition))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// ResourceGroup related
func WrapErrResourceGroupNotFound(rg any, msg ...string) error {
	err := wrapFields(ErrResourceGroupNotFound, value("rg", rg))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Replica related
func WrapErrReplicaNotFound(id int64, msg ...string) error {
	err := wrapFields(ErrReplicaNotFound, value("replica", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrReplicaNotAvailable(id int64, msg ...string) error {
	err := wrapFields(ErrReplicaNotAvailable, value("replica", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Channel related
func WrapErrChannelNotFound(name string, msg ...string) error {
	err := wrapFields(ErrChannelNotFound, value("channel", name))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrChannelLack(name string, msg ...string) error {
	err := wrapFields(ErrChannelLack, value("channel", name))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrChannelReduplicate(name string, msg ...string) error {
	err := wrapFields(ErrChannelReduplicate, value("channel", name))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrChannelNotAvailable(name string, msg ...string) error {
	err := wrapFields(ErrChannelNotAvailable, value("channel", name))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Segment related
func WrapErrSegmentNotFound(id int64, msg ...string) error {
	err := wrapFields(ErrSegmentNotFound, value("segment", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrSegmentsNotFound(ids []int64, msg ...string) error {
	err := wrapFields(ErrSegmentNotFound, value("segments", ids))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrSegmentNotLoaded(id int64, msg ...string) error {
	err := wrapFields(ErrSegmentNotLoaded, value("segment", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrSegmentLack(id int64, msg ...string) error {
	err := wrapFields(ErrSegmentLack, value("segment", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrSegmentReduplicate(id int64, msg ...string) error {
	err := wrapFields(ErrSegmentReduplicate, value("segment", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Index related
func WrapErrIndexNotFound(indexName string, msg ...string) error {
	err := wrapFields(ErrIndexNotFound, value("indexName", indexName))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrIndexNotFoundForSegment(segmentID int64, msg ...string) error {
	err := wrapFields(ErrIndexNotFound, value("segmentID", segmentID))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrIndexNotFoundForCollection(collection string, msg ...string) error {
	err := wrapFields(ErrIndexNotFound, value("collection", collection))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrIndexNotSupported(indexType string, msg ...string) error {
	err := wrapFields(ErrIndexNotSupported, value("indexType", indexType))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrIndexDuplicate(indexName string, msg ...string) error {
	err := wrapFields(ErrIndexDuplicate, value("indexName", indexName))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Node related
func WrapErrNodeNotFound(id int64, msg ...string) error {
	err := wrapFields(ErrNodeNotFound, value("node", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrNodeOffline(id int64, msg ...string) error {
	err := wrapFields(ErrNodeOffline, value("node", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrNodeLack(expectedNum, actualNum int64, msg ...string) error {
	err := wrapFields(ErrNodeLack,
		value("expectedNum", expectedNum),
		value("actualNum", actualNum),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrNodeLackAny(msg ...string) error {
	err := error(ErrNodeLack)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrNodeNotAvailable(id int64, msg ...string) error {
	err := wrapFields(ErrNodeNotAvailable, value("node", id))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrNodeNotMatch(expectedNodeID, actualNodeID int64, msg ...string) error {
	err := wrapFields(ErrNodeNotMatch,
		value("expectedNodeID", expectedNodeID),
		value("actualNodeID", actualNodeID),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// IO related
func WrapErrIoKeyNotFound(key string, msg ...string) error {
	err := wrapFields(ErrIoKeyNotFound, value("key", key))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrIoFailed(key string, err error) error {
	if err == nil {
		return nil
	}
	return wrapFieldsWithDesc(ErrIoFailed, err.Error(), value("key", key))
}

func WrapErrIoFailedReason(reason string, msg ...string) error {
	err := wrapFieldsWithDesc(ErrIoFailed, reason)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Parameter related
func WrapErrParameterInvalid[T any](expected, actual T, msg ...string) error {
	err := wrapFields(ErrParameterInvalid,
		value("expected", expected),
		value("actual", actual),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrParameterInvalidRange[T any](lower, upper, actual T, msg ...string) error {
	err := wrapFields(ErrParameterInvalid,
		bound("value", actual, lower, upper),
	)
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrParameterInvalidMsg(fmt string, args ...any) error {
	return errors.Wrapf(ErrParameterInvalid, fmt, args...)
}

// Metrics related
func WrapErrMetricNotFound(name string, msg ...string) error {
	err := wrapFields(ErrMetricNotFound, value("metric", name))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// Message queue related
func WrapErrMqTopicNotFound(name string, msg ...string) error {
	err := wrapFields(ErrMqTopicNotFound, value("topic", name))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrMqTopicNotEmpty(name string, msg ...string) error {
	err := wrapFields(ErrMqTopicNotEmpty, value("topic", name))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrMqInternal(err error, msg ...string) error {
	err = wrapFieldsWithDesc(ErrMqInternal, err.Error())
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrPrivilegeNotAuthenticated(fmt string, args ...any) error {
	err := errors.Wrapf(ErrPrivilegeNotAuthenticated, fmt, args...)
	return err
}

func WrapErrPrivilegeNotPermitted(fmt string, args ...any) error {
	err := errors.Wrapf(ErrPrivilegeNotPermitted, fmt, args...)
	return err
}

// Segcore related
func WrapErrSegcore(code int32, msg ...string) error {
	err := wrapFields(ErrSegcore, value("segcoreCode", code))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

// field related
func WrapErrFieldNotFound[T any](field T, msg ...string) error {
	err := wrapFields(ErrFieldNotFound, value("field", field))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func WrapErrFieldNameInvalid(field any, msg ...string) error {
	err := wrapFields(ErrFieldInvalidName, value("field", field))
	if len(msg) > 0 {
		err = errors.Wrap(err, strings.Join(msg, "->"))
	}
	return err
}

func wrapFields(err milvusError, fields ...errorField) error {
	for i := range fields {
		err.msg += fmt.Sprintf("[%s]", fields[i].String())
	}
	err.detail = err.msg
	return err
}

func wrapFieldsWithDesc(err milvusError, desc string, fields ...errorField) error {
	for i := range fields {
		err.msg += fmt.Sprintf("[%s]", fields[i].String())
	}
	err.msg += ": " + desc
	err.detail = err.msg
	return err
}

type errorField interface {
	String() string
}

type valueField struct {
	name  string
	value any
}

func value(name string, value any) valueField {
	return valueField{
		name,
		value,
	}
}

func (f valueField) String() string {
	return fmt.Sprintf("%s=%v", f.name, f.value)
}

type boundField struct {
	name  string
	value any
	lower any
	upper any
}

func bound(name string, value, lower, upper any) boundField {
	return boundField{
		name,
		value,
		lower,
		upper,
	}
}

func (f boundField) String() string {
	return fmt.Sprintf("%v out of range %v <= %s <= %v", f.value, f.lower, f.name, f.upper)
}
