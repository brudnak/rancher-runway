import {confluencePlanText} from './release-plan-text.mjs';
import * as pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';
import pdfWorkerSource from 'pdfjs-dist/legacy/build/pdf.worker.min.mjs?raw';
import mammoth from 'mammoth/mammoth.browser.js';

export async function readPlanFile(file){
 const extension=file.name.split('.').pop().toLowerCase();
 if(['doc','html','htm','mht','mhtml'].includes(extension))return confluencePlanText(await file.text());
 if(['txt','md'].includes(extension))return file.text();
 if(extension==='docx'){
  const result=await mammoth.extractRawText({arrayBuffer:await file.arrayBuffer()});
  if(!result.value.trim())throw new Error('No text found in this document. Paste the planner text instead.');
  return result.value;
 }
 if(extension!=='pdf')throw new Error('Choose a Confluence Word export, DOCX, PDF, or text file.');
 const workerURL=URL.createObjectURL(new Blob([pdfWorkerSource],{type:'text/javascript'}));
 pdfjs.GlobalWorkerOptions.workerSrc=workerURL;
 const task=pdfjs.getDocument({data:new Uint8Array(await file.arrayBuffer()),isEvalSupported:false,useSystemFonts:false});
 let timedOut=false;
 const timer=setTimeout(()=>{timedOut=true;void task.destroy();},30000);
 task.onPassword=()=>{void task.destroy();};
 try{
  const document=await task.promise;
  if(document.numPages>100)throw new Error('This plan has more than 100 pages. Export just the release-planning pages.');
  const pages=[];
  for(let number=1;number<=document.numPages;number++){
   const page=await document.getPage(number),content=await page.getTextContent();
   // Group nearby baselines, then read left-to-right to preserve table rows.
   const lines=[];
   for(const item of content.items){if(!('str' in item)||!item.str.trim())continue;const y=item.transform[5];let line=lines.find(row=>Math.abs(row.y-y)<3);if(!line){line={y,items:[]};lines.push(line);}line.items.push(item);}
   pages.push(lines.sort((a,b)=>b.y-a.y).map(line=>line.items.sort((a,b)=>a.transform[4]-b.transform[4]).map(item=>item.str).join(' ')).join('\n'));
   page.cleanup();
  }
  const text=pages.join('\n');if(!text.trim())throw new Error('This PDF has no selectable text. OCR is not available; paste the planner text or import a DOCX export.');return text;
 }catch(error){if(timedOut)throw new Error('PDF extraction timed out. Try a smaller export or paste the planner text.');throw error;}finally{clearTimeout(timer);await task.destroy();URL.revokeObjectURL(workerURL);}
}
