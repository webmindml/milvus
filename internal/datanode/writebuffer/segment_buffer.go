package writebuffer

import (
	"github.com/milvus-io/milvus-proto/go-api/v2/msgpb"
	"github.com/milvus-io/milvus-proto/go-api/v2/schemapb"
	"github.com/milvus-io/milvus/internal/storage"
	"github.com/milvus-io/milvus/pkg/util/typeutil"
)

type segmentBuffer struct {
	segmentID int64

	insertBuffer *InsertBuffer
	deltaBuffer  *DeltaBuffer
}

func newSegmentBuffer(segmentID int64, collSchema *schemapb.CollectionSchema) (*segmentBuffer, error) {
	insertBuffer, err := NewInsertBuffer(collSchema)
	if err != nil {
		return nil, err
	}
	return &segmentBuffer{
		segmentID:    segmentID,
		insertBuffer: insertBuffer,
		deltaBuffer:  NewDeltaBuffer(),
	}, nil
}

func (buf *segmentBuffer) IsFull() bool {
	return buf.insertBuffer.IsFull() || buf.deltaBuffer.IsFull()
}

func (buf *segmentBuffer) Yield() (insert *storage.InsertData, delete *storage.DeleteData) {
	return buf.insertBuffer.Yield(), buf.deltaBuffer.Yield()
}

func (buf *segmentBuffer) MinTimestamp() typeutil.Timestamp {
	insertTs := buf.insertBuffer.MinTimestamp()
	deltaTs := buf.deltaBuffer.MinTimestamp()

	if insertTs < deltaTs {
		return insertTs
	}
	return deltaTs
}

func (buf *segmentBuffer) EarliestPosition() *msgpb.MsgPosition {
	return getEarliestCheckpoint(buf.insertBuffer.startPos, buf.deltaBuffer.startPos)
}

// TimeRange is a range of timestamp contains the min-timestamp and max-timestamp
type TimeRange struct {
	timestampMin typeutil.Timestamp
	timestampMax typeutil.Timestamp
}

func getEarliestCheckpoint(cps ...*msgpb.MsgPosition) *msgpb.MsgPosition {
	var result *msgpb.MsgPosition
	for _, cp := range cps {
		if cp == nil {
			continue
		}
		if result == nil {
			result = cp
			continue
		}

		if cp.GetTimestamp() < result.GetTimestamp() {
			result = cp
		}
	}
	return result
}
