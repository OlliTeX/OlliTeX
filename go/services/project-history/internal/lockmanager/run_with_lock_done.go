package lockmanager

// RunWithLockDone — vendor LockManager.runWithLock(key, runner, done):
//
//	getLock → runner(extend, release) where release(err, args...) releases
//	the lock and forwards (err || releaseErr, args...) to done;
//	getLock failure → done(err);
//	done is invoked exactly once.
//
// The runner's second argument is the vendor `releaseLock(error, ...args)`;
// the done callback is the vendor third argument
// `(flushError, { queueSize, resyncNeeded } = {}) => ...`.
func (lm *LockManager) RunWithLockDone(
	key string,
	runner func(extend Extend, release func(error, ...any) error),
	done func(error, ...any),
) {
	if done == nil {
		done = func(error, ...any) {}
	}
	value, err := lm.GetLock(key)
	if err != nil {
		done(err)
		return
	}
	l := lm.lock(key, value)
	runner(l.Extend, func(err error, args ...any) error {
		releaseErr := l.Release()
		if err == nil {
			err = releaseErr
		}
		done(err, args...)
		return nil
	})
}
