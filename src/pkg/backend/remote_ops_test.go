package backend

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTransferSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1KB"},
		{1536, "2KB"},
		{1024 * 1024, "1.0MB"},
		{1024*1024 + 524288, "1.5MB"},
		{1 << 30, "1.0GB"},
		{(1 << 30) + (1 << 29), "1.5GB"},
	}

	for _, tc := range tests {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, FormatTransferSize(tc.bytes))
		})
	}
}

func TestProgressReader_ReportsProgress(t *testing.T) {
	data := "hello world this is a test"
	expectedTotal := int64(len(data))

	var mu sync.Mutex
	var reports []TransferProgress
	cb := func(tp TransferProgress) {
		mu.Lock()
		reports = append(reports, tp)
		mu.Unlock()
	}

	pr := &ProgressReader{
		reader:      strings.NewReader(data),
		total:       expectedTotal,
		callback:    cb,
		fileName:    "test.txt",
		minInterval: 0, // report every read
	}

	n, err := io.Copy(io.Discard, pr)
	require.NoError(t, err)
	assert.Equal(t, expectedTotal, n)

	mu.Lock()
	require.NotEmpty(t, reports, "should have at least one progress report")
	last := reports[len(reports)-1]
	mu.Unlock()

	assert.Equal(t, "test.txt", last.FileName)
	assert.Equal(t, expectedTotal, last.BytesCopied)
	assert.Equal(t, expectedTotal, last.TotalBytes)
}

func TestProgressReader_FiredDoneStopsAfterCompletion(t *testing.T) {
	data := "short"

	var mu sync.Mutex
	callCount := 0
	cb := func(tp TransferProgress) {
		mu.Lock()
		callCount++
		mu.Unlock()
	}

	pr := &ProgressReader{
		reader:      strings.NewReader(data),
		total:       int64(len(data)),
		callback:    cb,
		fileName:    "test.txt",
		minInterval: 0,
	}

	// Read everything
	_, err := io.Copy(io.Discard, pr)
	require.NoError(t, err)

	mu.Lock()
	firstCount := callCount
	mu.Unlock()
	assert.GreaterOrEqual(t, firstCount, 1, "should have called at least once")

	// Read again — firedDone should prevent new reports
	buf := make([]byte, 1)
	pr.Read(buf)

	mu.Lock()
	assert.Equal(t, firstCount, callCount, "firedDone should prevent further reports")
	mu.Unlock()
}

func TestProgressReader_EmptyFile(t *testing.T) {
	var reports []TransferProgress
	cb := func(tp TransferProgress) {
		reports = append(reports, tp)
	}

	pr := &ProgressReader{
		reader:      strings.NewReader(""),
		total:       0,
		callback:    cb,
		fileName:    "empty.txt",
		minInterval: 0,
	}

	_, err := io.Copy(io.Discard, pr)
	require.NoError(t, err)

	assert.Empty(t, reports, "empty file should not trigger progress reports")
}

func TestProgressReader_Throttling(t *testing.T) {
	data := strings.Repeat("a", 1024*1024) // 1MB
	callCount := 0

	cb := func(tp TransferProgress) {
		callCount++
	}

	pr := &ProgressReader{
		reader:      strings.NewReader(data),
		total:       int64(len(data)),
		callback:    cb,
		fileName:    "large.bin",
		minInterval: 50 * time.Millisecond,
	}

	_, err := io.Copy(io.Discard, pr)
	require.NoError(t, err)

	// With 50ms throttle, we should never fire more than ~20 times for 1MB
	// (each read is at most 32KB, so 32 reads, but throttled to ~1 per 50ms)
	assert.Less(t, callCount, 20, "throttle should prevent excessive callbacks")
	assert.GreaterOrEqual(t, callCount, 1, "should have at least one report")
}

func TestProgressReader_StallSignal(t *testing.T) {
	data := "data to transfer"
	stallCh := make(chan struct{}, 64)

	pr := &ProgressReader{
		reader:      strings.NewReader(data),
		total:       int64(len(data)),
		callback:    func(tp TransferProgress) {},
		fileName:    "test.txt",
		minInterval: 0,
		stallCh:     stallCh,
	}

	// Read one byte to trigger stall signal
	buf := make([]byte, 1)
	_, err := pr.Read(buf)
	require.NoError(t, err)

	select {
	case <-stallCh:
		// expected — signal was sent
	default:
		t.Error("expected stall signal after read")
	}
}

// safeReader wraps an io.Reader with a mutex so multiple goroutines
// can read safely (for testing concurrent ProgressReader access).
type safeReader struct {
	mu     sync.Mutex
	reader io.Reader
}

func (sr *safeReader) Read(p []byte) (int, error) {
	sr.mu.Lock()
	defer sr.mu.Unlock()
	return sr.reader.Read(p)
}

func TestProgressReader_ConcurrentSafe(t *testing.T) {
	data := strings.Repeat("b", 10000)
	var mu sync.Mutex
	var reports []TransferProgress
	cb := func(tp TransferProgress) {
		mu.Lock()
		reports = append(reports, tp)
		mu.Unlock()
	}

	pr := &ProgressReader{
		reader:      &safeReader{reader: strings.NewReader(data)},
		total:       int64(len(data)),
		callback:    cb,
		fileName:    "concurrent.txt",
		minInterval: 0,
	}

	// Simulate concurrent reads (should not race or corrupt state)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 100)
			for {
				n, err := pr.Read(buf)
				if n == 0 || err != nil {
					break
				}
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	require.NotEmpty(t, reports, "concurrent reads should still produce progress")
	last := reports[len(reports)-1]
	mu.Unlock()
	assert.Equal(t, int64(10000), last.BytesCopied, "final report should have full count")
}
