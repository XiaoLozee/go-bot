package ai

import (
	"context"
	"fmt"
	"reflect"

	"github.com/XiaoLozee/go-bot/internal/config"
)

func (s *Service) UpdateStorageConfig(ctx context.Context, storageCfg config.StorageConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	current := s.storageCfg
	logger := s.logger
	s.mu.RUnlock()
	if reflect.DeepEqual(current, storageCfg) {
		return nil
	}

	nextStore, err := openStore(ctx, storageCfg, logger)
	if err != nil {
		return fmt.Errorf("热更新 AI 存储连接失败: %w", err)
	}

	s.mu.Lock()
	oldStore := s.store
	s.storageCfg = storageCfg
	s.store = nextStore
	s.lastError = ""
	s.lastDecisionReason = "AI 存储配置已热更新"
	s.restorePersistedStateLocked(ctx)
	s.mu.Unlock()

	if oldStore != nil {
		_ = oldStore.Close()
	}
	return nil
}

func (s *Service) ensureStore(ctx context.Context) (Store, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.RLock()
	store := s.store
	storageCfg := s.storageCfg
	logger := s.logger
	s.mu.RUnlock()
	if store != nil {
		return store, nil
	}

	candidate, err := openStore(ctx, storageCfg, logger)
	if err != nil {
		s.mu.Lock()
		if s.store == nil {
			s.lastError = "AI 存储连接失败: " + err.Error()
			s.lastDecisionReason = "AI 存储未就绪，当前退化为内存模式"
		}
		s.mu.Unlock()
		return nil, fmt.Errorf("AI 聊天存储尚未初始化: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store != nil {
		existing := s.store
		_ = candidate.Close()
		return existing, nil
	}
	s.store = candidate
	s.lastError = ""
	s.lastDecisionReason = "AI 存储连接已恢复"
	s.restorePersistedStateLocked(ctx)
	return candidate, nil
}
