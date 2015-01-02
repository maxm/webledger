package main

import (
  "html/template"
  "path/filepath"
  "net/http"
)

var templates map[string]*template.Template

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
    templates[filepath.Base(view)] = template.Must(template.ParseFiles(files...))
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