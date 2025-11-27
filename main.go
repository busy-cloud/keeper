package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kardianos/service"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Name      string
	Command   string
	Arguments []string
	Cwd       string
	Delay     int
	Retry     int
}

var (
	help      bool
	install   bool
	uninstall bool
)

func init() {
	flag.BoolVar(&help, "h", false, "帮助")
	flag.BoolVar(&install, "i", false, "安装服务")
	flag.BoolVar(&uninstall, "u", false, "卸载服务")
}

var config Config
var svc service.Config

func load() error {
	fn := os.Args[0]
	ext := filepath.Ext(fn)
	if ext != "" {
		fn = strings.TrimSuffix(fn, ext)
	}
	fn += ".yaml" //keeper.yaml
	buf, err := os.ReadFile(fn)
	if err != nil {
		//创建默认文件
		log.Println("请写入配置文件")
		buf, _ = yaml.Marshal(&config)
		_ = os.WriteFile(fn, buf, 0666)

		return err
	}
	err = yaml.Unmarshal(buf, &config)
	return err
}

type Program struct {
	closed  bool
	process *os.Process
}

func (p *Program) run() {
	// 此处编写具体的服务代码
	hup := make(chan os.Signal, 2)
	signal.Notify(hup, syscall.SIGHUP)
	quit := make(chan os.Signal, 2)
	signal.Notify(quit, os.Interrupt, os.Kill)

	go func() {
		for {
			select {
			case <-hup:
			case <-quit:
				p.closed = true
				if p.process != nil {
					_ = p.process.Kill()
				}
				os.Exit(0)
			}
		}
	}()

	time.Sleep(time.Duration(config.Delay) * time.Second)

	attr := &os.ProcAttr{}
	attr.Files = append(attr.Files, os.Stdin, os.Stdout, os.Stderr)
	var err error

	//默认5秒启动一次
	if config.Retry == 0 {
		config.Retry = 5
	}

	//进程守护
	for !p.closed {
		p.process, err = os.StartProcess(config.Command, config.Arguments, attr)

		//启动失败重试
		if err != nil {
			log.Println(err)
		} else {
			_, _ = p.process.Wait()
			_ = p.process.Release()
			p.process = nil
		}

		//等下再重启
		time.Sleep(time.Second * time.Duration(config.Retry))
	}
}

func (p *Program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (p *Program) Stop(s service.Service) error {
	p.closed = true

	return nil
}

func main() {
	err := load()
	if err != nil {
		log.Fatal(err)
	}

	//构建服务
	svc.Name = config.Name
	svc.DisplayName = "Service Keeper of " + config.Name
	svc.Description = "Service Keeper" + config.Name
	svc.Arguments = config.Arguments

	var p Program
	s, err := service.New(&p, &svc)
	if err != nil {
		log.Fatal(err)
	}

	if install {
		err = s.Install()
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	if uninstall {
		err = s.Uninstall()
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	err = s.Run()
	if err != nil {
		log.Fatal(err)
	}
}
