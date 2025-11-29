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
	Name      string   `json:"name,omitempty"`
	Binary    string   `json:"binary,omitempty"`    //二进制文件
	Update    string   `json:"update,omitempty"`    //升级文件
	Arguments []string `json:"arguments,omitempty"` //参数
	Dir       string   `json:"dir,omitempty"`       //当前目录
	Delay     int      `json:"delay,omitempty"`     //延迟 s
	Retry     int      `json:"retry,omitempty"`     //重试 s
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

var config Config = Config{
	Name:   "demo",
	Binary: "demo",
	Delay:  5,
	Retry:  5,
}

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

func (p *Program) update() error {
	if config.Update == "" {
		return nil
	}

	info, err := os.Stat(config.Update)
	if os.IsNotExist(err) {
		return nil
	}
	if info.IsDir() {
		return nil
	}

	//清空上次备份
	_ = os.Remove(config.Binary + ".bak")

	//备份
	err = os.Rename(config.Binary, config.Binary+".bak")
	if err != nil {
		return err
	}

	//升级
	err = os.Rename(config.Update, config.Binary)
	if err != nil {
		log.Println("升级失败，恢复文件")
		_ = os.Rename(config.Binary+".bak", config.Binary) //恢复文件
		_ = os.Remove(config.Update)                       //删除升级文件
		return err
	}

	//升级后 启动失败
	err = p.execute()
	if err != nil {
		log.Println(err)
		log.Println("恢复到之前的文件")
		_ = os.Remove(config.Binary)
		_ = os.Rename(config.Binary+".bak", config.Binary)
	}

	_, _ = p.process.Wait()
	_ = p.process.Release()

	//等下再重启
	time.Sleep(time.Second * time.Duration(config.Retry))

	return nil
}

func (p *Program) execute() (err error) {
	attr := &os.ProcAttr{}
	attr.Env = os.Environ()
	attr.Dir = config.Dir
	attr.Files = append(attr.Files, os.Stdin, os.Stdout, os.Stderr)
	args := append([]string{config.Binary}, config.Arguments...)

	p.process, err = os.StartProcess(config.Binary, args, attr)
	return err
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

	//默认5秒启动一次
	if config.Retry == 0 {
		config.Retry = 5
	}

	//切换当前目录，默认是keeper同目录
	var err error
	dir := config.Dir
	if config.Dir == "" {
		dir = filepath.Dir(os.Args[0])
	}
	err = os.Chdir(dir)
	if err != nil {
		log.Fatal(err)
	}

	//进程守护
	for !p.closed {
		err = p.execute()

		//启动失败重试
		if err != nil {
			log.Println("启动进程失败", err)
		} else {
			log.Println("启动进程成功")
			_, _ = p.process.Wait()
			_ = p.process.Release()
			p.process = nil
		}

		//等下再重启
		time.Sleep(time.Second * time.Duration(config.Retry))

		//重启后才 检查升级
		err = p.update()
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
	flag.Parse()
	if help {
		flag.Usage()
		return
	}

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
