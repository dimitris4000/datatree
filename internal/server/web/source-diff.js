// Line tokens retain terminators so final-newline changes remain visible.
export function unifiedDiff(before, after) {
  if (before === after) return 'No changes since this file was opened.';
  const split = text => text.match(/[^\n]*\n|[^\n]+$/g) || [];
  const a = split(before), b = split(after), rows = [];
  let prefix = 0, suffix = 0;
  while (prefix < a.length && prefix < b.length && a[prefix] === b[prefix]) prefix++;
  while (suffix < a.length-prefix && suffix < b.length-prefix && a[a.length-1-suffix] === b[b.length-1-suffix]) suffix++;
  const push = (kind, text) => rows.push({kind, text});
  for (let i=0;i<prefix;i++) push(' ',a[i]);
  const n=a.length-prefix-suffix, m=b.length-prefix-suffix;
  // Bound quadratic work. A broad replacement is still a complete, valid diff.
  if ((n+1)*(m+1) <= 1000000) {
    const width=m+1, table=new Uint32Array((n+1)*width);
    for(let i=n-1;i>=0;i--)for(let j=m-1;j>=0;j--)
      table[i*width+j]=a[prefix+i]===b[prefix+j]?1+table[(i+1)*width+j+1]:Math.max(table[(i+1)*width+j],table[i*width+j+1]);
    let i=0,j=0;
    while(i<n||j<m){
      if(i<n&&j<m&&a[prefix+i]===b[prefix+j]){push(' ',a[prefix+i++]);j++;}
      else if(i<n&&(j===m||table[(i+1)*width+j]>=table[i*width+j+1]))push('-',a[prefix+i++]);
      else push('+',b[prefix+j++]);
    }
  } else {
    for(let i=prefix;i<a.length-suffix;i++)push('-',a[i]);
    for(let i=prefix;i<b.length-suffix;i++)push('+',b[i]);
  }
  for(let i=a.length-suffix;i<a.length;i++)push(' ',a[i]);
  let oldLine=1,newLine=1;
  for(const row of rows){row.old=oldLine;row.new=newLine;if(row.kind!=='+')oldLine++;if(row.kind!=='-')newLine++;}
  const hunks=[];
  for(let i=0;i<rows.length;i++)if(rows[i].kind!==' '){
    const start=Math.max(0,i-3),end=Math.min(rows.length,i+4),last=hunks.at(-1);
    if(last&&start<=last.end)last.end=end;else hunks.push({start,end});
  }
  const output=['--- Original\n+++ Working\n'];
  for(const {start,end} of hunks){
    const chunk=rows.slice(start,end),oldCount=chunk.filter(r=>r.kind!=='+').length,newCount=chunk.filter(r=>r.kind!=='-').length;
    const oldStart=rows[start].old-(oldCount===0?1:0),newStart=rows[start].new-(newCount===0?1:0);
    output.push(`@@ -${oldStart},${oldCount} +${newStart},${newCount} @@\n`);
    for(const row of chunk){output.push(row.kind+row.text);if(!row.text.endsWith('\n'))output.push('\n\\ No newline at end of file\n');}
  }
  return output.join('');
}
