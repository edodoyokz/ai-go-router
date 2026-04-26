package storage

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

type AsyncWriter struct {
	db         *DB
	requestCh  chan *RequestLog
	usageCh    chan *UsageRecord
	detailsCh  chan *RequestDetails
	bufferSize int
	flushDelay time.Duration
	wg         sync.WaitGroup
	logger     zerolog.Logger
}

type UsageRecord struct {
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
}

type RequestDetails struct {
	RequestID    string
	RequestBody  string
	ResponseBody string
	StatusCode   int
}

func NewAsyncWriter(db *DB, logger zerolog.Logger) *AsyncWriter {
	w := &AsyncWriter{
		db:         db,
		requestCh:  make(chan *RequestLog, 1000),
		usageCh:    make(chan *UsageRecord, 1000),
		detailsCh:  make(chan *RequestDetails, 1000),
		bufferSize: 100,
		flushDelay: 5 * time.Second,
		logger:     logger,
	}
	w.start()
	return w
}

func (w *AsyncWriter) start() {
	w.wg.Add(3)
	go w.processRequests()
	go w.processUsage()
	go w.processDetails()
}

// GetDB returns the underlying database connection
func (w *AsyncWriter) GetDB() *DB {
	return w.db
}

func (w *AsyncWriter) processRequests() {
	defer w.wg.Done()

	buffer := make([]*RequestLog, 0, w.bufferSize)
	ticker := time.NewTicker(w.flushDelay)
	defer ticker.Stop()

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, req := range buffer {
			if err := w.db.LogRequest(ctx, req); err != nil {
				w.logger.Error().Err(err).Str("request_id", req.RequestID).Msg("failed to log request")
			}
		}
		buffer = buffer[:0]
	}

	for {
		select {
		case req, ok := <-w.requestCh:
			if !ok {
				flush()
				return
			}
			buffer = append(buffer, req)
			if len(buffer) >= w.bufferSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (w *AsyncWriter) processUsage() {
	defer w.wg.Done()

	buffer := make([]*UsageRecord, 0, w.bufferSize)
	ticker := time.NewTicker(w.flushDelay)
	defer ticker.Stop()

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, usage := range buffer {
			if err := w.db.IncrementUsage(ctx, usage.Provider, usage.Model, usage.PromptTokens, usage.CompletionTokens); err != nil {
				w.logger.Error().Err(err).Str("provider", usage.Provider).Str("model", usage.Model).Msg("failed to increment usage")
			}
		}
		buffer = buffer[:0]
	}

	for {
		select {
		case usage, ok := <-w.usageCh:
			if !ok {
				flush()
				return
			}
			buffer = append(buffer, usage)
			if len(buffer) >= w.bufferSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (w *AsyncWriter) processDetails() {
	defer w.wg.Done()

	buffer := make([]*RequestDetails, 0, w.bufferSize)
	ticker := time.NewTicker(w.flushDelay)
	defer ticker.Stop()

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		for _, details := range buffer {
			if err := w.db.LogRequestDetails(ctx, details.RequestID, details.RequestBody, details.ResponseBody, details.StatusCode); err != nil {
				w.logger.Error().Err(err).Str("request_id", details.RequestID).Msg("failed to log request details")
			}
		}
		buffer = buffer[:0]
	}

	for {
		select {
		case details, ok := <-w.detailsCh:
			if !ok {
				flush()
				return
			}
			buffer = append(buffer, details)
			if len(buffer) >= w.bufferSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

func (w *AsyncWriter) LogRequest(req *RequestLog) {
	select {
	case w.requestCh <- req:
		// Successfully queued
	default:
		w.logger.Warn().
			Str("request_id", req.RequestID).
			Msg("async writer: request channel full, dropping log")
	}
}

func (w *AsyncWriter) IncrementUsage(provider, model string, promptTokens, completionTokens int) {
	usage := &UsageRecord{
		Provider:         provider,
		Model:            model,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
	}
	select {
	case w.usageCh <- usage:
		// Successfully queued
	default:
		w.logger.Warn().
			Str("provider", provider).
			Str("model", model).
			Msg("async writer: usage channel full, dropping usage record")
	}
}

func (w *AsyncWriter) LogRequestDetails(ctx context.Context, requestID, requestBody, responseBody string, statusCode int) {
	details := &RequestDetails{
		RequestID:    requestID,
		RequestBody:  requestBody,
		ResponseBody: responseBody,
		StatusCode:   statusCode,
	}
	select {
	case w.detailsCh <- details:
		// Successfully queued
	default:
		w.logger.Warn().
			Str("request_id", requestID).
			Msg("async writer: details channel full, dropping details log")
	}
}

func (w *AsyncWriter) Close() error {
	close(w.requestCh)
	close(w.usageCh)
	close(w.detailsCh)
	w.wg.Wait()
	return nil
}
