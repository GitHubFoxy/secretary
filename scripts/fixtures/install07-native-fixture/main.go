package main

import (
 "fmt"
 "os"
)

func main() {
 if len(os.Args) > 1 && os.Args[1] == "--version" {
  fmt.Println("install07-native-fixture 1.0")
  return
 }
 if len(os.Args) > 2 && os.Args[1] == "login" && os.Args[2] == "status" {
  fmt.Fprintln(os.Stderr, "not logged in")
  os.Exit(1)
 }
 os.Exit(2)
}
