package namespace_lock_utils

type LockState string

const (
	Unknown  LockState = "unknown"
	Locked   LockState = "locked"
	Unlocked LockState = "unlocked"
)

type NamespaceCache struct {
	cache map[string]LockState
}

func NewNamespaceCache() *NamespaceCache {
	return &NamespaceCache{make(map[string]LockState)}
}

func (nc *NamespaceCache) MarkLockStateUnlocked(namespace string) {
	nc.cache[namespace] = Unlocked
}

func (nc *NamespaceCache) MarkLockStateLocked(namespace string) {
	nc.cache[namespace] = Locked
}

func (nc *NamespaceCache) GetLockState(namespace string) LockState {
	if lockState, ok := nc.cache[namespace]; ok {
		return lockState
	} else {
		return Unknown
	}
}
