package files

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"strings"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/jobs"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	ThumbnailKind         = "THUMBNAIL_SMALL"
	ThumbnailMaxEdge      = 512
	ThumbnailMaxDimension = 16384
	ThumbnailMaxPixels    = 64_000_000
)

type Thumbnailer struct {
	pool  *pgxpool.Pool
	store storage.Adapter
	ids   id.Generator
	now   func() time.Time
}

func NewThumbnailer(pool *pgxpool.Pool, store storage.Adapter, ids id.Generator, now func() time.Time) *Thumbnailer {
	return &Thumbnailer{pool: pool, store: store, ids: ids, now: now}
}

type thumbnailSource struct {
	id, derivativeID                uuid.UUID
	key, mime, status               string
	derivativeKey, derivativeStatus string
}

func (t *Thumbnailer) Handle(ctx context.Context, job jobs.Job) error {
	if job.SubjectID == nil || job.SubjectType != jobs.SubjectFileObject {
		return jobs.Permanent("THUMBNAIL_SUBJECT_INVALID", "thumbnail subject is invalid")
	}
	source, noop, err := t.prepare(ctx, *job.SubjectID)
	if err != nil || noop {
		return err
	}
	finalKey := storage.Key(source.derivativeKey)
	if valid, mime, size, validateErr := t.validateFinal(ctx, finalKey); validateErr != nil {
		var handlerErr *jobs.HandlerError
		if errors.As(validateErr, &handlerErr) && handlerErr.Permanent {
			return t.permanentFailure(ctx, source.derivativeID, handlerErr.Code, handlerErr.Summary)
		}
		return validateErr
	} else if valid {
		return t.markReady(ctx, source, mime, size)
	}
	_ = t.store.Delete(ctx, storage.CommitTempKey(source.derivativeID))
	input, err := t.store.Open(ctx, storage.Key(source.key))
	if errors.Is(err, storage.ErrNotFound) {
		return jobs.Retryable("STORAGE_INTEGRITY_ERROR", "source object is missing")
	}
	if err != nil {
		return jobs.Retryable("STORAGE_UNAVAILABLE", "storage is unavailable")
	}
	defer func() { _ = input.Close() }()
	config, format, err := image.DecodeConfig(bufio.NewReader(input))
	if err != nil {
		return t.permanentFailure(ctx, source.derivativeID, "THUMBNAIL_DECODE_FAILED", "source image cannot be decoded")
	}
	if err = validateDimensions(config.Width, config.Height, ThumbnailMaxDimension, ThumbnailMaxPixels); err != nil {
		return t.permanentFailure(ctx, source.derivativeID, "THUMBNAIL_LIMIT_EXCEEDED", "source image exceeds thumbnail limits")
	}
	if !formatMatchesMIME(format, source.mime) {
		return t.permanentFailure(ctx, source.derivativeID, "THUMBNAIL_FORMAT_UNSUPPORTED", "source image format is unsupported")
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		return jobs.Retryable("STORAGE_UNAVAILABLE", "storage is unavailable")
	}
	orientation := 1
	if format == "jpeg" {
		orientation = jpegOrientation(input)
		if _, err = input.Seek(0, io.SeekStart); err != nil {
			return jobs.Retryable("STORAGE_UNAVAILABLE", "storage is unavailable")
		}
	}
	decoded, decodedFormat, err := image.Decode(bufio.NewReader(input))
	if err != nil || decodedFormat != format {
		return t.permanentFailure(ctx, source.derivativeID, "THUMBNAIL_DECODE_FAILED", "source image cannot be decoded")
	}
	decoded = orient(decoded, orientation)
	thumb := resizeFit(decoded, ThumbnailMaxEdge)
	tempKey := storage.CommitTempKey(source.derivativeID)
	temp, err := t.store.CreateCommitTemp(ctx, tempKey)
	if err != nil {
		return jobs.Retryable("STORAGE_UNAVAILABLE", "storage is unavailable")
	}
	generatedMime := "image/png"
	if format == "jpeg" {
		generatedMime = "image/jpeg"
		err = jpeg.Encode(temp, thumb, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(temp, thumb)
	}
	if err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = t.store.Delete(context.WithoutCancel(ctx), tempKey)
		return jobs.Retryable("STORAGE_UNAVAILABLE", "thumbnail storage write failed")
	}
	if err = t.store.Commit(ctx, tempKey, finalKey); err != nil {
		valid, mime, size, checkErr := t.validateFinal(ctx, finalKey)
		if checkErr != nil || !valid {
			return jobs.Retryable("STORAGE_UNAVAILABLE", "thumbnail storage commit failed")
		}
		return t.markReady(ctx, source, mime, size)
	}
	info, err := t.store.Stat(ctx, finalKey)
	if err != nil {
		return jobs.Retryable("STORAGE_UNAVAILABLE", "thumbnail storage is unavailable")
	}
	return t.markReady(ctx, source, generatedMime, info.Size())
}

func (t *Thumbnailer) prepare(ctx context.Context, sourceID uuid.UUID) (thumbnailSource, bool, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return thumbnailSource{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var s thumbnailSource
	err = tx.QueryRow(ctx, `SELECT id,storage_key,detected_mime,status FROM file_objects WHERE id=$1 FOR UPDATE`, sourceID).Scan(&s.id, &s.key, &s.mime, &s.status)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, true, tx.Commit(ctx)
	}
	if err != nil {
		return s, false, err
	}
	if s.status != "READY" {
		return s, true, tx.Commit(ctx)
	}
	if key := storage.Key(s.key); key.Validate() != nil || !strings.HasPrefix(s.key, "objects/") {
		return s, false, jobs.Permanent("STORAGE_INTEGRITY_ERROR", "source storage metadata is invalid")
	}
	if !jobs.IsThumbnailMIME(s.mime) {
		return s, false, jobs.Permanent("THUMBNAIL_FORMAT_UNSUPPORTED", "source image format is unsupported")
	}
	err = tx.QueryRow(ctx, `SELECT id,storage_key,status FROM file_derivatives WHERE source_file_id=$1 AND kind=$2 FOR UPDATE`, sourceID, ThumbnailKind).Scan(&s.derivativeID, &s.derivativeKey, &s.derivativeStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		s.derivativeID, err = t.ids.New()
		if err != nil {
			return s, false, err
		}
		s.derivativeKey = storage.DerivativeKey(s.derivativeID).String()
		_, err = tx.Exec(ctx, `INSERT INTO file_derivatives(id,source_file_id,kind,storage_key,mime,size_bytes,status,created_at,updated_at) VALUES($1,$2,$3,$4,'image/png',0,'PENDING',$5,$5)`, s.derivativeID, sourceID, ThumbnailKind, s.derivativeKey, t.now())
	} else if err == nil && s.derivativeStatus == "READY" {
		return s, true, tx.Commit(ctx)
	} else if err == nil && s.derivativeStatus == "FAILED" {
		_, err = tx.Exec(ctx, `UPDATE file_derivatives SET status='PENDING',size_bytes=0,updated_at=$2 WHERE id=$1`, s.derivativeID, t.now())
	}
	if err != nil {
		return s, false, err
	}
	return s, false, tx.Commit(ctx)
}

func (t *Thumbnailer) markReady(ctx context.Context, source thumbnailSource, mime string, size int64) error {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	err = tx.QueryRow(ctx, `SELECT status FROM file_objects WHERE id=$1 FOR UPDATE`, source.id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return t.cleanupUnownedDerivative(ctx, tx, source)
	}
	if err != nil {
		return err
	}
	if status != "READY" {
		return t.cleanupUnownedDerivative(ctx, tx, source)
	}
	ct, err := tx.Exec(ctx, `UPDATE file_derivatives SET status='READY',mime=$2,size_bytes=$3,updated_at=$4 WHERE id=$1 AND source_file_id=$5 AND status='PENDING'`, source.derivativeID, mime, size, t.now(), source.id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() != 1 {
		return errors.New("derivative state changed")
	}
	return tx.Commit(ctx)
}

func (t *Thumbnailer) cleanupUnownedDerivative(ctx context.Context, tx pgx.Tx, source thumbnailSource) error {
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return err
	}
	cleanupCtx := context.WithoutCancel(ctx)
	if err := t.store.Delete(cleanupCtx, storage.Key(source.derivativeKey)); err != nil {
		// Keep the PENDING derivative row when its source still exists. If a
		// concurrent delete already cascaded that row, the RUNNING job remains
		// durable authority and its retry transition will preserve a cleanup path.
		return jobs.Retryable("STORAGE_UNAVAILABLE", "thumbnail cleanup failed")
	}
	if _, err := t.pool.Exec(cleanupCtx, `DELETE FROM file_derivatives WHERE id=$1 AND status='PENDING'`, source.derivativeID); err != nil {
		return err
	}
	return nil
}

func (t *Thumbnailer) permanentFailure(ctx context.Context, derivativeID uuid.UUID, code, summary string) error {
	if _, err := t.pool.Exec(context.WithoutCancel(ctx), `UPDATE file_derivatives SET status='FAILED',updated_at=$2 WHERE id=$1 AND status='PENDING'`, derivativeID, t.now()); err != nil {
		return err
	}
	return jobs.Permanent(code, summary)
}

func (t *Thumbnailer) validateFinal(ctx context.Context, key storage.Key) (bool, string, int64, error) {
	info, err := t.store.Stat(ctx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return false, "", 0, nil
	}
	if err != nil {
		return false, "", 0, jobs.Retryable("STORAGE_UNAVAILABLE", "thumbnail storage is unavailable")
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return false, "", 0, jobs.Permanent("THUMBNAIL_OUTPUT_INVALID", "thumbnail output is invalid")
	}
	f, err := t.store.Open(ctx, key)
	if err != nil {
		return false, "", 0, jobs.Retryable("STORAGE_UNAVAILABLE", "thumbnail storage is unavailable")
	}
	defer func() { _ = f.Close() }()
	cfg, format, err := image.DecodeConfig(bufio.NewReader(f))
	if err != nil || (format != "jpeg" && format != "png") || validateDimensions(cfg.Width, cfg.Height, ThumbnailMaxEdge, ThumbnailMaxEdge*ThumbnailMaxEdge) != nil {
		return false, "", 0, jobs.Permanent("THUMBNAIL_OUTPUT_INVALID", "thumbnail output is invalid")
	}
	mime := "image/png"
	if format == "jpeg" {
		mime = "image/jpeg"
	}
	return true, mime, info.Size(), nil
}

func validateDimensions(width, height, maxDimension, maxPixels int) error {
	if width <= 0 || height <= 0 || width > maxDimension || height > maxDimension || width > maxPixels/height {
		return errors.New("image dimensions exceed limits")
	}
	return nil
}

func formatMatchesMIME(format, mime string) bool {
	return format == "jpeg" && mime == "image/jpeg" || format == "png" && mime == "image/png" || format == "gif" && mime == "image/gif" || format == "webp" && mime == "image/webp"
}

func resizeFit(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= maxEdge && height <= maxEdge {
		return src
	}
	scale := math.Min(float64(maxEdge)/float64(width), float64(maxEdge)/float64(height))
	dw, dh := max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func orient(src image.Image, orientation int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if orientation < 2 || orientation > 8 {
		return src
	}
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func jpegOrientation(r io.Reader) int {
	br := bufio.NewReader(r)
	marker := make([]byte, 2)
	if _, err := io.ReadFull(br, marker); err != nil || marker[0] != 0xff || marker[1] != 0xd8 {
		return 1
	}
	for {
		if _, err := io.ReadFull(br, marker); err != nil {
			return 1
		}
		if marker[0] != 0xff {
			return 1
		}
		for marker[1] == 0xff {
			if _, err := io.ReadFull(br, marker[1:]); err != nil {
				return 1
			}
		}
		if marker[1] == 0xda || marker[1] == 0xd9 {
			return 1
		}
		var lb [2]byte
		if _, err := io.ReadFull(br, lb[:]); err != nil {
			return 1
		}
		n := int(binary.BigEndian.Uint16(lb[:])) - 2
		if n < 0 || n > 1<<20 {
			return 1
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(br, data); err != nil {
			return 1
		}
		if marker[1] == 0xe1 && len(data) >= 14 && string(data[:6]) == "Exif\x00\x00" {
			if v := exifOrientation(data[6:]); v != 0 {
				return v
			}
		}
	}
}

func exifOrientation(data []byte) int {
	if len(data) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(data[2:4]) != 42 {
		return 0
	}
	offset := int(order.Uint32(data[4:8]))
	if offset < 0 || offset+2 > len(data) {
		return 0
	}
	count := int(order.Uint16(data[offset : offset+2]))
	base := offset + 2
	for i := 0; i < count; i++ {
		p := base + i*12
		if p+12 > len(data) {
			break
		}
		if order.Uint16(data[p:p+2]) == 0x0112 && order.Uint16(data[p+2:p+4]) == 3 {
			v := int(order.Uint16(data[p+8 : p+10]))
			if v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 0
}
