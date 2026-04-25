package controlplane

import "context"

func (s *PostgresStore) UpsertProvider(ctx context.Context, provider Provider, secret *ProviderSecret) (Provider, error) {
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

func (s *PostgresStore) SetProviderEnabled(ctx context.Context, id string, enabled bool) (Provider, error) {
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

func (s *PostgresStore) RotateProviderSecret(ctx context.Context, id string, secret ProviderSecret) (Provider, error) {
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

func (s *PostgresStore) DeleteProviderCredential(ctx context.Context, id string) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return Provider{}, err
	}
	provider, err := applyDeleteProviderCredential(ctx, &state, id)
	if err != nil {
		return Provider{}, err
	}
	if err := s.writeState(ctx, state); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (s *PostgresStore) DeleteProvider(ctx context.Context, id string) error {
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
