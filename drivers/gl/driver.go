// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gl

import (
	"container/list"
	"gaze/gxui"
	"gaze/gxui/math"
	"image"
	"io/ioutil"
	"path/filepath"
	"runtime"

	"github.com/go-gl/glfw3"
)

func init() {
	runtime.LockOSThread()
}

type Driver struct {
	pendingDriver chan func()
	pendingApp    chan func()
	viewports     *list.List
	dataPath      string
	debugEnabled  bool
}

type AppThread func(driver gxui.Driver)

func StartDriver(dataPath string, appThread AppThread) {
	if runtime.GOMAXPROCS(-1) < 2 {
		runtime.GOMAXPROCS(2)
	}

	if err := glfw3.Init(); err != nil {
		panic(err)
	}
	defer glfw3.Terminate()

	driver := &Driver{
		pendingDriver: make(chan func(), 256),
		pendingApp:    make(chan func(), 256),
		viewports:     list.New(),
		dataPath:      dataPath,
	}

	go appThread(driver)
	driver.run()
}

func (d *Driver) asyncDriver(f func()) {
	d.Call(f)
}

func (d *Driver) syncDriver(f func()) {
	c := make(chan bool, 1)
	d.Call(func() { f(); c <- true })
	<-c
}

func (d *Driver) createDriverEvent(signature interface{}) gxui.Event {
	return gxui.CreateChanneledEvent(signature, d.pendingDriver)
}

func (d *Driver) createAppEvent(signature interface{}) gxui.Event {
	return gxui.CreateChanneledEvent(signature, d.pendingApp)
}

func (d *Driver) flush() {
	for {
		select {
		case ev := <-d.pendingDriver:
			ev()
		default:
			return
		}
	}
}

func (d *Driver) run() {
	for {
		select {
		case ev, open := <-d.pendingDriver:
			if open {
				ev()
			} else {
				return // closed channel represents driver shutdown
			}
		default:
			glfw3.WaitEvents()
		}
	}
}

func (d *Driver) wake() {
	glfw3.PostEmptyEvent()
}

func (d *Driver) EnableDebug(enabled bool) {
	d.debugEnabled = enabled
}

// gxui.Driver compliance
func (d *Driver) Events() chan func() {
	return d.pendingApp
}

func (d *Driver) Call(f func()) {
	d.pendingDriver <- f
	d.wake()
}

func (d *Driver) Terminate() {
	d.asyncDriver(func() {
		for v := d.viewports.Front(); v != nil; v = v.Next() {
			v.Value.(*Viewport).Destroy()
		}
		d.flush()
		close(d.pendingDriver)
		close(d.pendingApp)
		d.viewports.Init()
	})
}

func (d *Driver) SetClipboard(str string) {
	d.asyncDriver(func() {
		v := d.viewports.Front().Value.(*Viewport)
		v.window.SetClipboardString(str)
	})
}

func (d *Driver) GetClipboard() (str string, err error) {
	d.syncDriver(func() {
		c := d.viewports.Front().Value.(*Viewport)
		str, err = c.window.GetClipboardString()
	})
	return
}

func (d *Driver) CreateViewport(width, height int, name string) gxui.Viewport {
	var v *Viewport
	d.syncDriver(func() {
		v = CreateViewport(d, width, height, name)
		e := d.viewports.PushBack(v)
		v.onDestroy.Listen(func() {
			d.viewports.Remove(e)
		})
	})
	return v
}

func (d *Driver) CreateCanvas(s math.Size) gxui.Canvas {
	return CreateCanvas(s)
}

func (d *Driver) CreateTexture(img image.Image, pixelsPerDip float32) gxui.Texture {
	return CreateTexture(img, pixelsPerDip)
}

func (d *Driver) CreateFont(name string, size int) gxui.Font {
	f, err := ioutil.ReadFile(filepath.Join(d.dataPath, name))
	if err != nil {
		panic(err)
	}
	return CreateFont(name, f, size)
}
