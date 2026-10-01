// A superseded request cannot publish progress or results into the current issue.
export function createIssueReadinessRequest(fetchReport,publish){
 let generation=0,controller=null;
 function cancel(){generation++;controller?.abort();controller=null;}
 return {cancel,async start(issueUrl,options={}){
  cancel();const ticket=generation,request=new AbortController();controller=request;
  const timer=setTimeout(()=>request.abort(),250000),startedAt=new Date().toISOString();let progress=[];
  const update=state=>{if(ticket===generation)publish({issueUrl,startedAt,progress,...state});};
  update({report:null,error:'',busy:true});
  try{const report=await fetchReport(issueUrl,request.signal,event=>{progress=[...progress,event].slice(-60);update({report:null,error:'',busy:true});},options);update({report,error:'',busy:false});}
  catch(err){update({report:null,error:err.name==='AbortError'?'Scan cancelled or timed out. You can try again.':err.message,busy:false});}
  finally{clearTimeout(timer);if(ticket===generation)controller=null;}
 }};
}
export async function readReadinessStream(response,onProgress){
 if(!response.body)throw new Error('The scan response could not be streamed.');
 const reader=response.body.getReader(),decoder=new TextDecoder();let buffer='',report;
 function consume(line){if(!line.trim())return;const event=JSON.parse(line);if(event.type==='progress')onProgress(event.progress);else if(event.type==='report')report=event.report;else if(event.type==='error')throw new Error(event.error);}
 try{while(true){const {value,done}=await reader.read();buffer+=decoder.decode(value,{stream:!done});if(buffer.length>16*1024*1024)throw new Error('Scan response exceeded its size limit.');let index;while((index=buffer.indexOf('\n'))>=0){consume(buffer.slice(0,index));buffer=buffer.slice(index+1);}if(done){consume(buffer);break;}}}
 catch(error){await reader.cancel().catch(()=>{});throw error;}finally{reader.releaseLock();}
 if(!report)throw new Error('The scan ended before a report arrived. Check again.');return report;
}
