(function() {
    var dateRegex = /^\d\d\d\d\/\d\d?\/\d\d?/;

    CodeMirror.ledgerHint = function(cm) {
        var cursor = cm.getCursor();
        var line = cm.getLine(cursor.line);
        var typed = line.substr(0, cursor.ch);
        var match
        if (line.length == 0 && cursor.line != 0 && cm.getLine(cursor.line-1).match(/^\s*$/)) {
            var previous = today();
            var next = today();
            for (var l = cursor.line; l > 0; --l) {
                var match = cm.getLine(l).match(dateRegex)
                if (match) {
                    previous = match[0]
                    break;
                }
            }
            for (var l = cursor.line; l < cm.lineCount(); ++l) {
                var match = cm.getLine(l).match(dateRegex)
                if (match) {
                    next = match[0]
                    break;
                }
            }
            return {
                list: next == previous ? [next] : [next, previous],
                from: cursor,
                to: cursor
            };
        } else if (typed.match(/^\s+\w[áéíóú\w\s:]*\s\s+$/)) {
            return {
                list: ['$','US$'],
                from: cursor,
                to: cursor
            };
        } else if (typed.match(/^\s+\w/) && !typed.match(/^\s+\w.*?\s\s/)) {
            account = typed.match(/^\s+(\w.*)/)[1];
            var accounts = $.grep(Accounts, function(s) { return s.match(new RegExp(account, "i")) });
            accounts = $.map(accounts, function(s) { return s + "  "; });
            return {
                list: accounts,
                from: {line: cursor.line, ch: typed.match(/^\s+/)[0].length },
                to: {line:cursor.line, ch: line.match(/^(\s+\w.*?)(\s\s|$)/)[1].length}
            }
        }
    };

    function today() {
        var date = new Date();
        return date.getFullYear() + "/" + zeroPad(date.getMonth() + 1) + "/" + zeroPad(date.getDate());
    };

    function zeroPad(num) {
        return num < 10 ? "0" + num : num;
    }

    CodeMirror.defineMode("ledger", function(config) {
        return {
            startState: function(base) {
              return {
                previousLineIsPosting: false,
                position: null
              };
            },
            token: function(stream, state) {
                if (stream.sol()) {
                    state.position = null;
                    state.previousLineIsPosting = false;
                    if(stream.match(dateRegex)) {
                        state.previousLineIsPosting = true;
                        state.position = "description";
                        return "qualifier";
                    }
                    if (stream.match(/\s\s+/)) {
                        state.position = "account";
                        return null;
                    }
                }
                if (state.position == "description") {
                    stream.skipToEnd();
                    state.position = null;
                    return null;
                }
                if (state.position == "account" && stream.match(/[a-z]/i)) {
                    while(!stream.eol() && !stream.match(/\s\s/, false)) stream.next();
                    state.previousLineIsPosting = true;
                    state.position = "amount"
                    stream.eatSpace();
                    return "keyword";
                }
                if (state.position == "amount" 
                    && (stream.match(/([a-z]|\$)+\s*-?\d+(\.\d+)?/i)
                     || stream.match(/-?\d+(\.\d+)?\s*([a-z]|\$)+/i))) {
                    state.position = null;
                    return "number";
                }
                state.position = null;
                stream.next()
                return null;
            },
            blankLine: function(state) {
                state.previousLineIsPosting = false;
            },
            indent: function(state, textAfter) {
                return state.previousLineIsPosting ? 2 : 0;
            }
        };
    });
})();

