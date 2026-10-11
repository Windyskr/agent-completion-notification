package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// jsonObject 是保留键顺序的 JSON 对象，值以原始字节保存。
//
// 改写用户的 settings.json 不能借道 map：encoding/json 会把 map 的键按字母重排，
// 还会把 &、<、> 转义成 & 一类形式，用户自己的 hook 命令随之面目全非。
type jsonObject struct {
	keys   []string
	values map[string]json.RawMessage
}

func newJSONObject() *jsonObject {
	return &jsonObject{values: map[string]json.RawMessage{}}
}

// parseJSONObject 按原始顺序解析顶层对象。重复键沿用 encoding/json 的语义：
// 后出现的值生效，位置保留在首次出现处。
func parseJSONObject(data []byte) (*jsonObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, errors.New("不是 JSON 对象")
	}
	obj := newJSONObject()
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("对象键 %v 不是字符串", tok)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		obj.set(key, value)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("JSON 对象之后存在多余内容")
	}
	return obj, nil
}

func (o *jsonObject) get(key string) (json.RawMessage, bool) {
	value, ok := o.values[key]
	return value, ok
}

// set 覆盖已有键的值并保持其位置；新键追加到末尾。
func (o *jsonObject) set(key string, value json.RawMessage) {
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *jsonObject) delete(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

func (o *jsonObject) empty() bool { return len(o.keys) == 0 }

// marshal 按键的原始顺序拼回紧凑 JSON，值字节原样写入。
func (o *jsonObject) marshal() (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodedKey, err := marshalJSON(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		buf.Write(o.values[key])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// marshalJSON 与 json.Marshal 相同，但不做 HTML 转义。
func marshalJSON(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

// rawArray 把若干原始 JSON 值拼成数组，各元素字节原样写入。
func rawArray(items []json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, item := range items {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(item)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}
