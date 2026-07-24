package game

import (
	"encoding/json"
	"reflect"
	"sort"
	"time"

	gsr "github.com/lijiawang/GameServiceRuntime/runtime"
)

type timelineFire struct {
	BattleID BattleID
	Epoch    BattleEpoch
	ID       TimelineID
	Revision TimelineRevision
	Command  gsr.CommandID
}

type timelineRecord struct {
	item    TimelineItem
	payload any
}

type battleTimeline struct {
	battle *BattleService
	nextID TimelineID
	items  map[TimelineID]timelineRecord
}

type timelineHandle struct {
	timeline *battleTimeline
	active   *bool
}

func (t timelineHandle) After(delay time.Duration, command gsr.CommandID, payload any) (TimelineID, error) {
	if !t.usable() {
		return 0, ErrContextExpired
	}
	if delay < 0 {
		return 0, ErrInvalidCommand
	}
	return t.schedule(t.timeline.battle.service.Now().Add(delay), command, payload)
}

func (t timelineHandle) At(at time.Time, command gsr.CommandID, payload any) (TimelineID, error) {
	if !t.usable() {
		return 0, ErrContextExpired
	}
	if at.IsZero() {
		return 0, ErrInvalidCommand
	}
	now := t.timeline.battle.service.Now()
	if at.Before(now) {
		at = now
	}
	return t.schedule(at, command, payload)
}

func (t timelineHandle) Replace(id TimelineID, delay time.Duration, command gsr.CommandID, payload any) (TimelineRevision, error) {
	if !t.usable() {
		return 0, ErrContextExpired
	}
	if delay < 0 {
		return 0, ErrInvalidCommand
	}
	record, exists := t.timeline.items[id]
	if !exists || record.item.State != TimelineScheduled || record.item.Revision == ^TimelineRevision(0) {
		return 0, ErrStateConflict
	}
	return t.replace(id, t.timeline.battle.service.Now().Add(delay), command, payload)
}

func (t timelineHandle) Cancel(id TimelineID) bool {
	if !t.usable() {
		return false
	}
	record, exists := t.timeline.items[id]
	if !exists || record.item.State != TimelineScheduled {
		return false
	}
	record.item.State = TimelineCancelled
	t.timeline.items[id] = record
	return true
}

func (t timelineHandle) Snapshot() TimelineSnapshot {
	if !t.usable() {
		return TimelineSnapshot{}
	}
	return t.timeline.snapshot()
}

func (t timelineHandle) usable() bool { return t.timeline != nil && t.active != nil && *t.active }

func (t timelineHandle) schedule(dueAt time.Time, command gsr.CommandID, payload any) (TimelineID, error) {
	if !validTimelineCommand(command) {
		return 0, ErrInvalidCommand
	}
	copyPayload, err := cloneTimelinePayload(payload)
	if err != nil {
		return 0, err
	}
	if t.timeline.nextID == ^TimelineID(0) {
		return 0, ErrStateConflict
	}
	id := t.timeline.nextID + 1
	record := timelineRecord{item: TimelineItem{ID: id, Revision: 1, DueAt: dueAt, Command: command, State: TimelineScheduled}, payload: copyPayload}
	if err := t.timeline.schedule(record); err != nil {
		return 0, err
	}
	t.timeline.nextID = id
	t.timeline.items[id] = record
	return id, nil
}

func (t timelineHandle) replace(id TimelineID, dueAt time.Time, command gsr.CommandID, payload any) (TimelineRevision, error) {
	if !validTimelineCommand(command) {
		return 0, ErrInvalidCommand
	}
	copyPayload, err := cloneTimelinePayload(payload)
	if err != nil {
		return 0, err
	}
	record := t.timeline.items[id]
	record.item.Revision++
	record.item.DueAt = dueAt
	record.item.Command = command
	record.item.State = TimelineScheduled
	record.payload = copyPayload
	if err := t.timeline.schedule(record); err != nil {
		return 0, err
	}
	t.timeline.items[id] = record
	return record.item.Revision, nil
}

func (t *battleTimeline) schedule(record timelineRecord) error {
	delay := record.item.DueAt.Sub(t.battle.service.Now())
	if delay < 0 {
		delay = 0
	}
	_, err := t.battle.service.After(delay, TimelineFireCommand, timelineFire{BattleID: t.battle.id, Epoch: t.battle.epoch, ID: record.item.ID, Revision: record.item.Revision, Command: record.item.Command})
	return err
}

func (t *battleTimeline) snapshot() TimelineSnapshot {
	items := make([]TimelineItem, 0, len(t.items))
	for _, record := range t.items {
		items = append(items, record.item)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].ID < items[right].ID })
	return TimelineSnapshot{NextID: t.nextID, Items: items}
}

func (t *battleTimeline) cancelAll() {
	for id, record := range t.items {
		if record.item.State == TimelineScheduled {
			record.item.State = TimelineCancelled
			t.items[id] = record
		}
	}
}

func validTimelineCommand(command gsr.CommandID) bool {
	return command != 0 && !reservedBattleCommand(command)
}

func cloneTimelinePayload(payload any) (any, error) {
	if payload == nil {
		return nil, nil
	}
	if _, err := json.Marshal(payload); err != nil {
		return nil, ErrInvalidCommand
	}
	clone, ok := cloneValue(reflect.ValueOf(payload))
	if !ok {
		return nil, ErrInvalidCommand
	}
	return clone.Interface(), nil
}

func cloneValue(value reflect.Value) (reflect.Value, bool) {
	if !value.IsValid() {
		return value, true
	}
	switch value.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.String:
		return value, true
	case reflect.Interface:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		inner, ok := cloneValue(value.Elem())
		if !ok {
			return reflect.Value{}, false
		}
		result := reflect.New(value.Type()).Elem()
		result.Set(inner)
		return result, true
	case reflect.Pointer:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		inner, ok := cloneValue(value.Elem())
		if !ok {
			return reflect.Value{}, false
		}
		result := reflect.New(value.Type().Elem())
		result.Elem().Set(inner)
		return result, true
	case reflect.Slice:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		result := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for index := 0; index < value.Len(); index++ {
			item, ok := cloneValue(value.Index(index))
			if !ok {
				return reflect.Value{}, false
			}
			result.Index(index).Set(item)
		}
		return result, true
	case reflect.Array:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.Len(); index++ {
			item, ok := cloneValue(value.Index(index))
			if !ok {
				return reflect.Value{}, false
			}
			result.Index(index).Set(item)
		}
		return result, true
	case reflect.Map:
		if value.IsNil() {
			return reflect.Zero(value.Type()), true
		}
		result := reflect.MakeMapWithSize(value.Type(), value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			key, keyOK := cloneValue(iterator.Key())
			item, itemOK := cloneValue(iterator.Value())
			if !keyOK || !itemOK {
				return reflect.Value{}, false
			}
			result.SetMapIndex(key, item)
		}
		return result, true
	case reflect.Struct:
		result := reflect.New(value.Type()).Elem()
		for index := 0; index < value.NumField(); index++ {
			if !result.Field(index).CanSet() {
				return reflect.Value{}, false
			}
			item, ok := cloneValue(value.Field(index))
			if !ok {
				return reflect.Value{}, false
			}
			result.Field(index).Set(item)
		}
		return result, true
	default:
		return reflect.Value{}, false
	}
}
