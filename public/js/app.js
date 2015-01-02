$(document).ready(function(){
  var $fullFile = $('.full-file');
  if($fullFile.length > 0){
    CodeMirror.commands.autocomplete = function(cm) {
      CodeMirror.showHint(cm, CodeMirror.ledgerHint, { completeSingle: false });
    }
    var codeMirror = CodeMirror.fromTextArea($fullFile[0], {
      extraKeys: {
        "Ctrl-Space": "autocomplete",
      }
    });
    codeMirror.on("change", function(cm, change) {
        CodeMirror.commands.autocomplete(cm);
    });
    codeMirror.focus();
    codeMirror.setCursor({line: codeMirror.lineCount()})
  }

  var copyTemplate = function(){
    if($('.template').length > 0){
      var html = $('.template').html().replace(/placeholder/g, (new Date()).getTime());
      var $el = $('<div>'+html+'</div>');
      $('.lines').append($el);
    }
  }
  copyTemplate();
  $(document).on('keydown', '.lines > div:last-child input', function(){
    copyTemplate();
  });
  $('form').submit(function(){
    $('.template').remove();
  });
});
