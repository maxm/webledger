package main

import (
  "html/template"
  "path/filepath"
  "net/http"
)

var templates map[string]*template.Template

// Template functions
var templateFuncs = template.FuncMap{
  "add": func(a, b float64) float64 { return a + b },
  "sub": func(a, b float64) float64 { return a - b },
  "mul": func(a, b float64) float64 { return a * b },
  "div": func(a, b float64) float64 {
    if b == 0 {
      return 0
    }
    return a / b
  },
}

func InitTemplates() {
  if templates == nil {
    templates = make(map[string]*template.Template)
  }

  layouts, err := filepath.Glob("templates/layout/*.tmpl")
  if err != nil {
    Log("%v", err)
  }

  views, err := filepath.Glob("templates/views/*.tmpl")
  if err != nil {
    Log("%v", err)
  }

  // Generate our templates map from our layouts/ and includes/ directories
  for _, view := range views {
    files := append(layouts, view)
    templates[filepath.Base(view)] = template.Must(template.New(filepath.Base(view)).Funcs(templateFuncs).ParseFiles(files...))
  }
}

func RenderTemplate(w http.ResponseWriter, name string, data map[string]interface{}) {
  name += ".tmpl"
  // Ensure the template exists in the map.
  tmpl, ok := templates[name]
  if !ok {
    Log("The template %s does not exist.", name)
    return
  }

  w.Header().Set("Content-Type", "text/html; charset=utf-8")
  err := tmpl.ExecuteTemplate(w, "layout", data)
  if err != nil {
    Log("%v", err)
    return
  }

  return
}