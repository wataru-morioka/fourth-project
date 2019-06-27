package main
 
import (
    "fmt"
    "database/sql"
    _"github.com/mattn/go-oci8"
)
 
func main(){
 
    db, err := sql.Open("oci8", "Go/go@oracle-nodeport:1521/ThirdProject")
    if err != nil {
        fmt.Println(err)
        return
    }
    defer db.Close()
     
     
    rows,err := db.Query("select * from test")
    if err != nil {
        fmt.Println("Error running query")
        fmt.Println(err)
        return
    }
    defer rows.Close()
 
    for rows.Next() {
        var Id string
        var Name string
        rows.Scan(&Id, &Name)
        fmt.Printf("The date is: %s\n", Name)
    }
}