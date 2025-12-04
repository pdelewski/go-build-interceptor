package main

import "fmt"

func bar2() {
}

func bar1() {
  bar2()
}

func foo() {
  bar1()
}

func main() {
  fmt.Println("hello")
  foo()
}