package main

// yaml格式也需要有预定义的结构体
type Eye struct {
	Servers   []string
	Port      int
	WebPort   int
	Threads   int
	N         int
	W         int
	R         int
	Buckets   int
	Slow      int
	Listen    string
	Proxies   []string
	AccessLog string
	ErrorLog  string
	Basepath  string
	Readonly  bool
}
