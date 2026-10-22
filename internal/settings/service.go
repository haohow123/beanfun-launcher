package settings

// Service is the Wails-bound facade that lets the UI ask whether the settings file is healthy.
type Service struct {
	file *File
}

func NewService(f *File) *Service {
	return &Service{file: f}
}

// Status re-reads the settings file and reports its health.
func (s *Service) Status() Status {
	return s.file.Status()
}
