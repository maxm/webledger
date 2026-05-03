$(document).ready(function(){
  CodeMirror.commands.autocomplete = function(cm) {
    CodeMirror.showHint(cm, CodeMirror.ledgerHint, { completeSingle: false });
  }
  $('.full-file').each(function(){
    var ta = this;
    var editor = CodeMirror.fromTextArea(ta, {
      extraKeys: {
        "Ctrl-Space": "autocomplete",
      }
    });
    editor.on("change", function(cm, change) {
      CodeMirror.commands.autocomplete(cm);
    });
    $(ta).data('cm', editor);
    if($(ta).hasClass('auto-focus')){
      editor.focus();
      editor.setCursor({line: editor.lineCount()});
    }
  });

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
