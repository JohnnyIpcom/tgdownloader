package config

import "time"

type Config interface {
	Load(name string, path string) error

	IsSet(key string) bool
	Get(key string) interface{}
	GetString(key string) string
	GetBool(key string) bool
	GetInt(key string) int
	GetInt32(key string) int32
	GetInt64(key string) int64
	GetUint(key string) uint
	GetUint32(key string) uint32
	GetUint64(key string) uint64
	GetFloat64(key string) float64
	GetTime(key string) time.Time
	GetDuration(key string) time.Duration
	GetSlice(key string) []interface{}
	GetIntSlice(key string) []int
	GetStringSlice(key string) []string
	GetStringMap(key string) map[string]interface{}
	GetStringMapString(key string) map[string]string
	GetStringMapStringSlice(key string) map[string][]string
	GetSizeInBytes(key string) uint

	Set(key string, value interface{})
	SetDefault(key string, defaultValue interface{})

	Unmarshal(rawVal interface{}) error

	Sub(key string) Config
}
