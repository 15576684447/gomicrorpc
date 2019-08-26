package learn

/*
比较好的博客地址：
https://me.csdn.net/u013272009
micro/go-micro 介绍与源代码分析（一）:
	https://blog.csdn.net/u013272009/article/details/93315904
micro/go-micro 介绍与源代码分析（二）: 命令行参数与 Option 机制
	https://blog.csdn.net/u013272009/article/details/93382080
micro/go-micro 介绍与源代码分析（三）: service 模块
	https://blog.csdn.net/u013272009/article/details/93632947
micro/go-micro 介绍与源代码分析（四）: transport 模块
	https://blog.csdn.net/u013272009/article/details/94772751

 */


//go micro中普遍使用的参数构造模型
/*
比如说我想制作一杯咖啡,(糖,牛奶,咖啡粉,盐,酒,冰淇淋,蜂蜜...)
这些原料应该是可以选择添加的.
那么：一杯咖啡就可以随意添加自己喜欢的原料了
*/
type CoffeeOption func(Opts *CoffeeOptions)

type CoffeeOptions struct {
	sugar        int
	milk         int
	coffeePowder int
}

type Coffee struct {
	opts *CoffeeOptions
}

func CoffeeSugar(sugar int) CoffeeOption {
	return func(opts *CoffeeOptions) {
		opts.sugar = sugar
	}
}

func CoffeeMilk(milk int) CoffeeOption {
	return func(opts *CoffeeOptions) {
		opts.milk = milk
	}
}

func CoffeeCoffeePowder(coffeePowder int) CoffeeOption {
	return func(opts *CoffeeOptions) {
		opts.coffeePowder = coffeePowder
	}
}

func newDefaultCoffeeOptions() *CoffeeOptions {
	return &CoffeeOptions{
		sugar:        2,
		milk:         5,
		coffeePowder: 100,
	}
}

func NewCoffee(opts ...CoffeeOption) *Coffee {
	defaultOptions := newDefaultCoffeeOptions()
	for _, opt := range opts {
		opt(defaultOptions)
	}
	return &Coffee{
		opts: defaultOptions,
	}
}
