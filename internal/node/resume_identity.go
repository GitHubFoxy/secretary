package node

import "context"

func resumeSameIdentity(ctx context.Context, runtime Resumer, request StartRequest, nativeID string) (Session, error) {
	session, err := runtime.Resume(ctx, request, nativeID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.ID() != nativeID {
		if session != nil {
			_ = session.Close()
		}
		return nil, ErrRuntimeSessionUnavailable
	}
	return session, nil
}
