package controlplane

import "context"

func (s *FileStore) UpsertProvider(ctx context.Context, provider Provider, secret *ProviderSecret) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	provider, err := applyProviderUpsert(ctx, &s.data, provider, secret)
	if err != nil {
		return Provider{}, err
	}
	if err := s.persistLocked(); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *FileStore) SetProviderEnabled(ctx context.Context, id string, enabled bool) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	provider, err := applySetProviderEnabled(ctx, &s.data, id, enabled)
	if err != nil {
		return Provider{}, err
	}
	if err := s.persistLocked(); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *FileStore) RotateProviderSecret(ctx context.Context, id string, secret ProviderSecret) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	provider, err := applyRotateProviderSecret(ctx, &s.data, id, secret)
	if err != nil {
		return Provider{}, err
	}
	if err := s.persistLocked(); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *FileStore) DeleteProvider(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := applyDeleteProvider(ctx, &s.data, id); err != nil {
		return err
	}
	return s.persistLocked()
}

func (s *RedisStore) UpsertProvider(ctx context.Context, provider Provider, secret *ProviderSecret) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	provider, err = applyProviderUpsert(ctx, &state, provider, secret)
	if err != nil {
		return Provider{}, err
	}
	if err := s.writeState(ctx, state); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *RedisStore) SetProviderEnabled(ctx context.Context, id string, enabled bool) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	provider, err := applySetProviderEnabled(ctx, &state, id, enabled)
	if err != nil {
		return Provider{}, err
	}
	if err := s.writeState(ctx, state); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *RedisStore) RotateProviderSecret(ctx context.Context, id string, secret ProviderSecret) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	provider, err := applyRotateProviderSecret(ctx, &state, id, secret)
	if err != nil {
		return Provider{}, err
	}
	if err := s.writeState(ctx, state); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *RedisStore) DeleteProvider(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return err
	}
	if err := applyDeleteProvider(ctx, &state, id); err != nil {
		return err
	}
	return s.writeState(ctx, state)
}
