package runtime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	relationAnalysisTaskStatusQueued    = "queued"
	relationAnalysisTaskStatusRunning   = "running"
	relationAnalysisTaskStatusSucceeded = "succeeded"
	relationAnalysisTaskStatusFailed    = "failed"
	relationAnalysisTaskStatusCanceled  = "canceled"
)

type aiRelationAnalysisTask struct {
	ID         string
	GroupID    string
	Force      bool
	Status     string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
	Result     *AIRelationAnalysisResult
	Error      string
	Message    string
}

func (s *Service) StartAIRelationAnalysis(_ context.Context, req AIRelationAnalysisRequest) (AIRelationAnalysisTaskView, error) {
	if s.aiService == nil {
		return AIRelationAnalysisTaskView{}, fmt.Errorf("AI 核心未初始化")
	}
	groupID := strings.TrimSpace(req.GroupID)
	now := time.Now()

	s.relationTaskMu.Lock()
	if s.relationTasks == nil {
		s.relationTasks = make(map[string]*aiRelationAnalysisTask)
	}
	s.pruneRelationTasksLocked()
	if existing := s.findActiveRelationTaskLocked(groupID); existing != nil {
		view := existing.view()
		view.Accepted = true
		if strings.TrimSpace(view.Message) == "" {
			view.Message = "已有 AI 关系分析任务正在执行"
		}
		s.relationTaskMu.Unlock()
		return view, nil
	}
	s.relationTaskSeq++
	taskID := fmt.Sprintf("rel-%d-%d", now.UnixMilli(), s.relationTaskSeq)
	task := &aiRelationAnalysisTask{
		ID:        taskID,
		GroupID:   groupID,
		Force:     req.Force,
		Status:    relationAnalysisTaskStatusQueued,
		CreatedAt: now,
		Message:   "AI 关系分析任务已提交，等待执行",
	}
	s.relationTasks[taskID] = task
	view := task.view()
	view.Accepted = true
	s.relationTaskMu.Unlock()

	taskCtx, tracked := s.relationAnalysisTaskContext()
	if tracked {
		s.wg.Add(1)
	}
	go func() {
		if tracked {
			defer s.wg.Done()
		}
		s.runAIRelationAnalysisTask(taskCtx, taskID, AIRelationAnalysisRequest{GroupID: groupID, Force: req.Force})
	}()
	return view, nil
}

func (s *Service) GetAIRelationAnalysisTask(_ context.Context, taskID string) (AIRelationAnalysisTaskView, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return AIRelationAnalysisTaskView{}, fmt.Errorf("任务 ID 不能为空")
	}
	s.relationTaskMu.RLock()
	task := s.relationTasks[taskID]
	if task == nil {
		s.relationTaskMu.RUnlock()
		return AIRelationAnalysisTaskView{}, fmt.Errorf("AI 关系分析任务不存在: %s", taskID)
	}
	view := task.view()
	s.relationTaskMu.RUnlock()
	view.Accepted = true
	return view, nil
}

func (s *Service) relationAnalysisTaskContext() (context.Context, bool) {
	s.mu.RLock()
	runCtx := s.runCtx
	running := s.state == StateRunning && runCtx != nil
	s.mu.RUnlock()
	if running {
		return runCtx, true
	}
	return context.Background(), false
}

func (s *Service) runAIRelationAnalysisTask(ctx context.Context, taskID string, req AIRelationAnalysisRequest) {
	startedAt := time.Now()
	s.relationTaskMu.Lock()
	if task := s.relationTasks[taskID]; task != nil {
		task.Status = relationAnalysisTaskStatusRunning
		task.StartedAt = &startedAt
		task.Message = "AI 正在分析关系图谱与群友画像"
	}
	s.relationTaskMu.Unlock()

	result, err := s.AnalyzeAIRelations(ctx, req)
	finishedAt := time.Now()

	s.relationTaskMu.Lock()
	defer s.relationTaskMu.Unlock()
	task := s.relationTasks[taskID]
	if task == nil {
		return
	}
	task.FinishedAt = &finishedAt
	if err != nil {
		if errors.Is(err, context.Canceled) {
			task.Status = relationAnalysisTaskStatusCanceled
			task.Error = "AI 关系分析任务已取消"
			task.Message = task.Error
		} else {
			task.Status = relationAnalysisTaskStatusFailed
			task.Error = err.Error()
			task.Message = err.Error()
		}
		if s.logger != nil {
			s.logger.Error("AI 关系分析后台任务失败", "task_id", taskID, "group_id", req.GroupID, "error", err)
		}
		return
	}
	resultCopy := result
	task.Status = relationAnalysisTaskStatusSucceeded
	task.Result = &resultCopy
	task.Error = ""
	task.Message = strings.TrimSpace(result.Message)
	if task.Message == "" {
		task.Message = "AI 关系分析已生成"
	}
}

func (s *Service) findActiveRelationTaskLocked(groupID string) *aiRelationAnalysisTask {
	for _, task := range s.relationTasks {
		if task == nil {
			continue
		}
		if task.GroupID != groupID {
			continue
		}
		if task.Status == relationAnalysisTaskStatusQueued || task.Status == relationAnalysisTaskStatusRunning {
			return task
		}
	}
	return nil
}

func (s *Service) pruneRelationTasksLocked() {
	if len(s.relationTasks) < maxAIRelationTasks {
		return
	}
	tasks := make([]*aiRelationAnalysisTask, 0, len(s.relationTasks))
	for _, task := range s.relationTasks {
		if task != nil {
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	for _, task := range tasks {
		if len(s.relationTasks) <= maxAIRelationTasks {
			return
		}
		if task.Status == relationAnalysisTaskStatusQueued || task.Status == relationAnalysisTaskStatusRunning {
			continue
		}
		delete(s.relationTasks, task.ID)
	}
	for _, task := range tasks {
		if len(s.relationTasks) <= maxAIRelationTasks {
			return
		}
		delete(s.relationTasks, task.ID)
	}
}

func (t *aiRelationAnalysisTask) view() AIRelationAnalysisTaskView {
	if t == nil {
		return AIRelationAnalysisTaskView{}
	}
	view := AIRelationAnalysisTaskView{
		TaskID:    t.ID,
		Status:    t.Status,
		GroupID:   t.GroupID,
		Force:     t.Force,
		CreatedAt: t.CreatedAt,
		Error:     t.Error,
		Message:   t.Message,
	}
	if t.StartedAt != nil {
		startedAt := *t.StartedAt
		view.StartedAt = &startedAt
	}
	if t.FinishedAt != nil {
		finishedAt := *t.FinishedAt
		view.FinishedAt = &finishedAt
	}
	if t.Result != nil {
		resultCopy := *t.Result
		view.Result = &resultCopy
		if strings.TrimSpace(view.Message) == "" {
			view.Message = resultCopy.Message
		}
	}
	return view
}
