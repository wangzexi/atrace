package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type InputError struct{ msg string }

func (e *InputError) Error() string { return e.msg }

func badf(format string, args ...any) error {
	return &InputError{fmt.Sprintf(format, args...)}
}

// IsInputError identifies invalid collector input for HTTP adapters.
func IsInputError(err error) bool {
	var input *InputError
	return errors.As(err, &input)
}

// Input is what a collector submits: one ActivityStreams document and what it
// observed about it. The document is either an activity or a content object.
type Input struct {
	ObservedAt time.Time       `json:"observed_at"`
	ObservedBy string          `json:"observed_by,omitempty"`
	Raw        json.RawMessage `json:"raw,omitempty"`
	Document   json.RawMessage `json:"document"`
}

// Activity types Atrace accepts. Like, Announce, Add and Create assert a
// state; Undo and Remove retract it.
var positiveActivities = map[string]bool{"Create": true, "Like": true, "Announce": true, "Add": true}
var undoable = map[string]bool{"Like": true, "Announce": true}

const maxString = 1000
const maxContent = 1 << 20

type parsedInput struct {
	observedAt time.Time
	observedBy string
	raw        string
	typ        string
	doc        map[string]any
	canonical  []byte
	hash       string
	activity   *activityColumns // set for activities
	object     *objectColumns   // set for content objects
}

type objectColumns struct {
	id           string
	attributedTo string
	inReplyTo    string
	quote        string
	publishedAt  string
	updatedAt    string
}

type activityColumns struct {
	kind        string
	actor       string
	object      string
	target      string
	publishedAt string
	announceID  string
}

func parseInput(raw json.RawMessage) (*parsedInput, error) {
	var in Input
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	if in.ObservedAt.IsZero() {
		return nil, badf("observed_at is required")
	}
	if len(in.ObservedBy) > maxString {
		return nil, badf("observed_by is too long")
	}
	if len(in.Raw) != 0 && !json.Valid(in.Raw) {
		return nil, badf("raw must be valid JSON")
	}
	doc, err := decodeDocument(in.Document)
	if err != nil {
		return nil, err
	}
	typ, err := stringField(doc, "type")
	if err != nil {
		return nil, err
	}
	if typ == "" || len(typ) > 100 {
		return nil, badf("document needs a type")
	}
	p := &parsedInput{observedAt: in.ObservedAt.UTC(), observedBy: in.ObservedBy, typ: typ, doc: doc}
	if len(in.Raw) != 0 {
		p.raw = string(in.Raw)
	}
	switch {
	case positiveActivities[typ] || typ == "Undo" || typ == "Remove":
		p.activity, err = parseActivity(typ, doc)
	default:
		p.object, err = parseObject(doc)
	}
	if err != nil {
		return nil, err
	}
	p.canonical, err = canonicalJSON(doc)
	if err != nil {
		return nil, err
	}
	if len(p.canonical) > maxContent {
		return nil, badf("document exceeds %d bytes", maxContent)
	}
	sum := sha256.Sum256(p.canonical)
	p.hash = hex.EncodeToString(sum[:])
	return p, nil
}

func parseObject(doc map[string]any) (*objectColumns, error) {
	id, err := stringField(doc, "id")
	if err != nil {
		return nil, err
	}
	if !validRef(id) {
		return nil, badf("id must look like platform:kind:id")
	}
	cols := &objectColumns{id: id}
	for field, dst := range map[string]*string{"attributedTo": &cols.attributedTo, "inReplyTo": &cols.inReplyTo, "quote": &cols.quote} {
		if *dst, err = refField(doc, field); err != nil {
			return nil, err
		}
	}
	for field, dst := range map[string]*string{"published": &cols.publishedAt, "updated": &cols.updatedAt} {
		if *dst, err = timeField(doc, field); err != nil {
			return nil, err
		}
	}
	if content, err := stringField(doc, "content"); err != nil {
		return nil, err
	} else if len(content) > maxContent {
		return nil, badf("content exceeds %d bytes", maxContent)
	}
	if err := validateAttachments(doc["attachment"]); err != nil {
		return nil, err
	}
	return cols, nil
}

func parseActivity(typ string, doc map[string]any) (*activityColumns, error) {
	id, err := stringField(doc, "id")
	if err != nil {
		return nil, err
	}
	if len(id) > maxString {
		return nil, badf("id is too long")
	}
	actor, err := refField(doc, "actor")
	if err != nil {
		return nil, err
	}
	if actor == "" {
		return nil, badf("%s needs an actor", typ)
	}
	cols := &activityColumns{kind: typ, actor: actor}
	if cols.publishedAt, err = timeField(doc, "published"); err != nil {
		return nil, err
	}
	if cols.target, err = refField(doc, "target"); err != nil {
		return nil, err
	}
	switch typ {
	case "Undo":
		inner, ok := doc["object"].(map[string]any)
		if !ok {
			return nil, badf("Undo.object must be the activity being undone")
		}
		innerType, err := stringField(inner, "type")
		if err != nil {
			return nil, err
		}
		if !undoable[innerType] {
			return nil, badf("Undo can only undo Like or Announce; use Remove for Add")
		}
		innerActor, err := refField(inner, "actor")
		if err != nil {
			return nil, err
		}
		if innerActor != "" && innerActor != actor {
			return nil, badf("Undo and the undone activity must have the same actor")
		}
		if cols.object, err = refField(inner, "object"); err != nil {
			return nil, err
		}
		cols.kind = innerType
		if innerType == "Announce" {
			cols.announceID, err = stringField(inner, "id")
			if err != nil {
				return nil, err
			}
			if len(cols.announceID) > maxString {
				return nil, badf("Undo.object.id is too long")
			}
		}
		if cols.target != "" {
			return nil, badf("Undo has no target")
		}
	default:
		if typ == "Announce" {
			cols.announceID = id
		}
		if cols.object, err = refField(doc, "object"); err != nil {
			return nil, err
		}
		if typ == "Remove" {
			cols.kind = "Add"
		}
	}
	if cols.object == "" {
		return nil, badf("%s needs an object", typ)
	}
	if (typ == "Add" || typ == "Remove") && cols.target == "" {
		return nil, badf("%s needs a target", typ)
	}
	if typ != "Add" && typ != "Remove" && cols.target != "" {
		return nil, badf("%s has no target", typ)
	}
	return cols, nil
}

// validateAttachments checks the shape of attachment media. Whether a local
// copy exists is the collector's choice and lives in the media table.
func validateAttachments(value any) error {
	if value == nil {
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		return badf("attachment must be an array")
	}
	for i, item := range list {
		a, ok := item.(map[string]any)
		if !ok {
			return badf("attachment[%d] must be an object", i)
		}
		typ, err := stringField(a, "type")
		if err != nil {
			return err
		}
		if typ != "Image" && typ != "Video" && typ != "Audio" {
			return badf("attachment[%d].type must be Image, Video or Audio", i)
		}
		link, err := stringField(a, "url")
		if err != nil {
			return err
		}
		if link == "" {
			return badf("attachment[%d] needs a url", i)
		}
		if err := validateURL(fmt.Sprintf("attachment[%d].url", i), link); err != nil {
			return err
		}
	}
	return nil
}

func stringField(doc map[string]any, field string) (string, error) {
	value, ok := doc[field]
	if !ok || value == nil {
		return "", nil
	}
	s, ok := value.(string)
	if !ok {
		return "", badf("%s must be a string", field)
	}
	return s, nil
}

// refField reads a reference to another document. References are IDs only;
// embedded objects are not stored, so submit them as their own documents.
func refField(doc map[string]any, field string) (string, error) {
	value, ok := doc[field]
	if !ok || value == nil {
		return "", nil
	}
	s, ok := value.(string)
	if !ok {
		return "", badf("%s must be an ID string; submit embedded objects as their own documents", field)
	}
	if !validRef(s) {
		return "", badf("%s must look like platform:kind:id", field)
	}
	return s, nil
}

func timeField(doc map[string]any, field string) (string, error) {
	s, err := stringField(doc, field)
	if err != nil || s == "" {
		return "", err
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return "", badf("%s must be an RFC 3339 time", field)
	}
	return formatTime(t), nil
}

func validateURL(field, value string) error {
	if len(value) > maxString {
		return badf("%s is too long", field)
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return badf("%s must be an absolute HTTP(S) URL", field)
	}
	return nil
}

func decodeStrict(raw json.RawMessage, value any) error {
	if len(raw) == 0 {
		return badf("missing JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return badf("invalid input: %v", err)
	}
	return nil
}

// decodeDocument keeps numbers exact so the stored document equals what the
// collector sent.
func decodeDocument(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, badf("document is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil || doc == nil {
		return nil, badf("document must be a JSON object")
	}
	return doc, nil
}

// canonicalJSON serializes with sorted keys, so equal documents hash equally.
func canonicalJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// validRef checks the platform:kind:id shape shared by every document ID.
func validRef(value string) bool {
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 || !validPlatform(parts[0]) || !validPlatform(parts[1]) || parts[2] == "" || strings.TrimSpace(parts[2]) != parts[2] || len(value) > maxString {
		return false
	}
	return true
}

// ValidRef checks the platform:kind:id format of a document ID.
func ValidRef(value string) bool { return validRef(value) }

func validPlatform(platform string) bool {
	if platform == "" {
		return false
	}
	for _, char := range platform {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

const timeLayout = "2006-01-02T15:04:05.000000Z"

func formatTime(value time.Time) string { return value.UTC().Format(timeLayout) }
