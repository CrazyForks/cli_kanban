package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/happytaoer/cli_kanban/internal/db"
	"github.com/happytaoer/cli_kanban/internal/model"
)

// ViewMode represents the current view mode
type ViewMode int

const (
	ViewModeBoard ViewMode = iota
	ViewModeAddTask
	ViewModeEditTask
	ViewModeEditDescription
	ViewModeEditTags
	ViewModeEditDue
	ViewModeConfirmDelete
	ViewModeHelp
	ViewModeSearch
	ViewModeStats
)

// FocusArea tracks which pane currently owns input focus.
type FocusArea int

const (
	FocusAreaBoard FocusArea = iota
	FocusAreaDetail
)

// DetailField identifies a field in the right-side detail panel.
type DetailField int

const (
	DetailFieldNone DetailField = iota
	DetailFieldTitle
	DetailFieldDescription
	DetailFieldTags
	DetailFieldDue
)

// Model is the main TUI model
type Model struct {
	db              *db.DB
	columns         []model.Column
	currentColumn   int
	currentTask     int
	scrollOffsets   []int // scroll offset per column
	viewMode        ViewMode
	focusArea       FocusArea
	detailField     DetailField
	editingField    DetailField
	currentTime     time.Time
	pendingDeleteID int64 // task ID pending deletion confirmation
	followTaskID    int64 // task ID to follow after reload
	textInput       textinput.Model
	titleInput      textinput.Model
	tagsInput       textinput.Model
	textArea        textarea.Model
	searchInput     textinput.Model
	dueInput        textinput.Model
	selectedTaskID  int64
	searchQuery     string // active search filter
	stats           model.TaskStats
	viewport        viewport.Model
	width           int
	height          int
	ready           bool // viewport ready flag
	err             error
}

// clockTickCmd creates a command that emits time ticks every second
func clockTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return clockTickMsg(t)
	})
}

// NewModel creates a new TUI model
func NewModel(database *db.DB) Model {
	ti := textinput.New()
	ti.Placeholder = "Enter task title..."
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 50

	titleInput := textinput.New()
	titleInput.Placeholder = "Task title"
	titleInput.CharLimit = 200
	titleInput.Width = 30

	tagsInput := textinput.New()
	tagsInput.Placeholder = "bug, urgent, feature"
	tagsInput.CharLimit = 200
	tagsInput.Width = 30

	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Placeholder = "Enter task description..."
	ta.SetWidth(80)
	ta.SetHeight(10)
	ta.CharLimit = 2000

	si := textinput.New()
	si.Placeholder = "Search tasks..."
	si.CharLimit = 100
	si.Width = 30

	di := textinput.New()
	di.Placeholder = "YYYY-MM-DD (leave empty to clear)"
	di.CharLimit = 20
	di.Width = 30

	return Model{
		db:            database,
		columns:       model.GetAllColumns(),
		currentColumn: 0,
		currentTask:   0,
		scrollOffsets: make([]int, 3), // one per column
		currentTime:   time.Now(),
		viewMode:      ViewModeBoard,
		focusArea:     FocusAreaBoard,
		textInput:     ti,
		titleInput:    titleInput,
		tagsInput:     tagsInput,
		textArea:      ta,
		searchInput:   si,
		dueInput:      di,
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadTasks(), clockTickCmd())
}

// loadTasks loads all tasks from the database
func (m Model) loadTasks() tea.Cmd {
	return func() tea.Msg {
		tasks, err := m.db.GetAllTasks()
		if err != nil {
			return errMsg{err}
		}
		return tasksLoadedMsg{tasks}
	}
}

// Messages
type tasksLoadedMsg struct {
	tasks []model.Task
}

type taskCreatedMsg struct {
	task *model.Task
}

type taskUpdatedMsg struct{}

type taskDeletedMsg struct{}

type descriptionUpdatedMsg struct{}

type tagsUpdatedMsg struct{}

type dueUpdatedMsg struct{}

type clockTickMsg time.Time

type errMsg struct {
	err error
}

// maxVisibleTasks is the maximum number of tasks visible per column
const maxVisibleTasks = 10

const (
	fixedBoardColumnWidth = 30
	fixedBoardGap         = 1
	fixedDetailPaneWidth  = 72
	fixedBoardPaneWidth   = fixedBoardColumnWidth*3 + fixedBoardGap*2
	minSplitWidth         = fixedBoardPaneWidth + 1 + fixedDetailPaneWidth
)

// shouldShowDetailPane reports whether the split layout is active.
func (m Model) shouldShowDetailPane() bool {
	width := m.width
	if width <= 0 {
		width = 80
	}
	return width >= minSplitWidth
}

// detailPaneWidth returns the width reserved for the detail panel.
func (m Model) detailPaneWidth() int {
	return fixedDetailPaneWidth
}

// boardPaneWidth returns the width reserved for the kanban board.
func (m Model) boardPaneWidth() int {
	return fixedBoardPaneWidth
}

// resizeDetailInputs updates editor widths based on the active layout.
func (m *Model) resizeDetailInputs() {
	if !m.shouldShowDetailPane() {
		return
	}

	detailWidth := m.detailPaneWidth()
	if detailWidth <= 0 {
		return
	}

	singleLineWidth := detailWidth - 8
	if singleLineWidth < 18 {
		singleLineWidth = 18
	}
	m.titleInput.Width = singleLineWidth
	m.tagsInput.Width = singleLineWidth
	m.dueInput.Width = singleLineWidth

	textAreaWidth := detailWidth - 6
	if textAreaWidth < 18 {
		textAreaWidth = 18
	}
	m.textArea.SetWidth(textAreaWidth)

	textAreaHeight := m.height - 18
	if textAreaHeight < 6 {
		textAreaHeight = 6
	}
	m.textArea.SetHeight(textAreaHeight)
}

// getCurrentTask returns the currently selected task (respecting active filters)
func (m *Model) getCurrentTask() *model.Task {
	if len(m.columns) == 0 || m.currentColumn < 0 || m.currentColumn >= len(m.columns) {
		return nil
	}

	visibleIndices := m.visibleTaskIndices(m.currentColumn)
	if len(visibleIndices) == 0 || m.currentTask < 0 || m.currentTask >= len(visibleIndices) {
		return nil
	}

	actualIdx := visibleIndices[m.currentTask]
	col := &m.columns[m.currentColumn]
	if actualIdx < 0 || actualIdx >= len(col.Tasks) {
		return nil
	}

	return &col.Tasks[actualIdx]
}

// getDetailTask returns the task currently shown in the detail panel.
func (m *Model) getDetailTask() *model.Task {
	if m.editingField != DetailFieldNone && m.selectedTaskID != 0 {
		task, _, _ := m.findTaskByID(m.selectedTaskID)
		if task != nil {
			return task
		}
	}
	return m.getCurrentTask()
}

// visibleTaskCount returns the number of tasks visible in the current filter.
func (m *Model) visibleTaskCount(columnIndex int) int {
	return len(m.visibleTaskIndices(columnIndex))
}

// syncSelectedTask keeps the detail pane bound to the current visible selection.
func (m *Model) syncSelectedTask() {
	if task := m.getCurrentTask(); task != nil {
		m.selectedTaskID = task.ID
		return
	}
	m.selectedTaskID = 0
}

// clearDetailEditing resets the right-side field editing state.
func (m *Model) clearDetailEditing() {
	m.focusArea = FocusAreaBoard
	m.detailField = DetailFieldNone
	m.editingField = DetailFieldNone
	m.titleInput.Blur()
	m.tagsInput.Blur()
	m.textArea.Blur()
	m.dueInput.Blur()
}

// beginDetailEdit activates field-level editing in the right-side panel.
func (m *Model) beginDetailEdit(field DetailField) bool {
	if !m.shouldShowDetailPane() {
		return false
	}

	task := m.getCurrentTask()
	if task == nil {
		return false
	}

	m.clearDetailEditing()
	m.resizeDetailInputs()
	m.selectedTaskID = task.ID
	m.focusArea = FocusAreaDetail
	m.detailField = field
	m.editingField = field

	switch field {
	case DetailFieldTitle:
		m.titleInput.SetValue(task.Title)
		m.titleInput.Focus()
	case DetailFieldDescription:
		m.textArea.SetValue(task.Description)
		m.textArea.Focus()
	case DetailFieldTags:
		m.tagsInput.SetValue(strings.Join(task.Tags, ", "))
		m.tagsInput.Focus()
	case DetailFieldDue:
		if task.Due != nil {
			m.dueInput.SetValue(task.Due.Format("2006-01-02"))
		} else {
			m.dueInput.SetValue("")
		}
		m.dueInput.Focus()
	default:
		m.clearDetailEditing()
		return false
	}

	return true
}

// findTaskByID locates a task in the board columns.
func (m *Model) findTaskByID(id int64) (*model.Task, int, int) {
	if id == 0 {
		return nil, -1, -1
	}

	for colIdx := range m.columns {
		for taskIdx := range m.columns[colIdx].Tasks {
			if m.columns[colIdx].Tasks[taskIdx].ID == id {
				return &m.columns[colIdx].Tasks[taskIdx], colIdx, taskIdx
			}
		}
	}

	return nil, -1, -1
}

// selectTaskByID restores the current selection using the visible task index.
func (m *Model) selectTaskByID(id int64) bool {
	if id == 0 {
		return false
	}

	for colIdx := range m.columns {
		visible := m.visibleTaskIndices(colIdx)
		for visibleIdx, actualIdx := range visible {
			if actualIdx < 0 || actualIdx >= len(m.columns[colIdx].Tasks) {
				continue
			}
			if m.columns[colIdx].Tasks[actualIdx].ID == id {
				m.currentColumn = colIdx
				m.currentTask = visibleIdx
				m.selectedTaskID = id
				m.ensureTaskVisible()
				return true
			}
		}
	}

	return false
}

// ensureTaskVisible adjusts scroll offset to keep current task visible
func (m *Model) ensureTaskVisible() {
	if len(m.columns) == 0 {
		return
	}

	visibleIndices := m.visibleTaskIndices(m.currentColumn)
	visibleCount := len(visibleIndices)

	if visibleCount == 0 {
		m.currentTask = 0
		m.scrollOffsets[m.currentColumn] = 0
		return
	}

	if m.currentTask >= visibleCount {
		m.currentTask = visibleCount - 1
	}
	if m.currentTask < 0 {
		m.currentTask = 0
	}

	offset := m.scrollOffsets[m.currentColumn]
	maxOffset := visibleCount - maxVisibleTasks
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}

	if m.currentTask < offset {
		offset = m.currentTask
	}
	if m.currentTask >= offset+maxVisibleTasks {
		offset = m.currentTask - maxVisibleTasks + 1
	}

	m.scrollOffsets[m.currentColumn] = offset
}

// organizeTasks organizes tasks into columns by status
func (m *Model) organizeTasks(tasks []model.Task) {
	// Reset all columns
	for i := range m.columns {
		m.columns[i].Tasks = []model.Task{}
	}

	// Organize tasks by status
	for _, task := range tasks {
		for i := range m.columns {
			if m.columns[i].Status == task.Status {
				m.columns[i].Tasks = append(m.columns[i].Tasks, task)
				break
			}
		}
	}

	// If we're following a task after move, find its position
	if m.followTaskID != 0 {
		found := m.selectTaskByID(m.followTaskID)
		m.followTaskID = 0 // Clear after finding
		if found {
			return
		}
	}

	if m.selectedTaskID != 0 && m.selectTaskByID(m.selectedTaskID) {
		return
	}

	// Ensure currentTask/scroll offset are valid for the current filters
	m.ensureTaskVisible()
	m.syncSelectedTask()
}

// visibleTaskIndices returns the indices of tasks visible in the given column
// after applying the current search filter.
func (m Model) visibleTaskIndices(columnIndex int) []int {
	if columnIndex < 0 || columnIndex >= len(m.columns) {
		return nil
	}

	col := m.columns[columnIndex]
	if m.searchQuery == "" {
		indices := make([]int, len(col.Tasks))
		for i := range col.Tasks {
			indices[i] = i
		}
		return indices
	}

	indices := make([]int, 0, len(col.Tasks))
	for i, task := range col.Tasks {
		if m.matchesSearch(task) {
			indices = append(indices, i)
		}
	}
	return indices
}
