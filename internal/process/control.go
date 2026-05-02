package process

import "memodroid/internal/driver"

func Attach(drv driver.Driver, pid int) error   { return drv.Attach(pid) }
func Detach(drv driver.Driver, pid int)         { drv.Detach(pid) }
func Stop(drv driver.Driver, pid int) error     { return drv.Stop(pid) }
func Continue(drv driver.Driver, pid int) error { return drv.Continue(pid) }
