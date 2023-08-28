package namespace_lock_utils

import "sync"

type NamespaceLockMap struct {
	M     map[string]*sync.Mutex
	Mutex *sync.Mutex
}

func (nlm *NamespaceLockMap) getLock(key string) *sync.Mutex {
	nlm.Mutex.Lock()
	defer nlm.Mutex.Unlock()

	if _, exists := nlm.M[key]; !exists {
		nlm.M[key] = &sync.Mutex{}
	}
	return nlm.M[key]
}

func (nlm *NamespaceLockMap) Lock(key string) {
	nlm.getLock(key).Lock()
}

func (nlm *NamespaceLockMap) Unlock(key string) {
	nlm.getLock(key).Unlock()
}
