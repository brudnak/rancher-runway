package test

func (p *localControlPanel) stopCacheLab() {
	p.cacheLabMu.Lock()
	s := p.cacheLab
	p.cacheLabMu.Unlock()
	if s != nil {
		s.Stop()
	}
}
