(function(root){
    const LIMIT=200;
    function retain(items,direction){
        const pending=items.filter(item=>item.sending);
        const messages=items.filter(item=>!item.sending);
        const size=Math.max(0,LIMIT-pending.length);
        const overflow=messages.length>size;
        return {
            items:(size ? (direction==='older'?messages.slice(0,size):messages.slice(-size)) : []).concat(pending),
            older:overflow && direction!=='older', newer:overflow && direction==='older',
        };
    }
    function visibleRange(heights,scrollTop,height,overscan=4){
        let first=0,offset=0;
        while(first<heights.length && offset+heights[first]<=scrollTop){offset+=heights[first++];}
        let end=first,total=offset;
        while(end<heights.length && total<scrollTop+height){total+=heights[end++];}
        const start=Math.max(0,first-overscan);
        end=Math.min(heights.length,end+overscan);
        return {start,end,top:heights.slice(0,start).reduce((a,b)=>a+b,0),bottom:heights.slice(end).reduce((a,b)=>a+b,0)};
    }
    const component={
        props:{items:{type:Array,required:true},older:Boolean,newer:Boolean,loading:Boolean},
        data(){return {range:{start:0,end:20,top:0,bottom:0}};},
        created(){
            this._sizes=new Map(); this._keys=new WeakMap(); this._sequence=0;
            this._observed=new Set(); this._anchor=null; this._bottom=true;
            this._frame=0; this._settle=0; this._restoring=false; this._dead=false; this._edge='';
        },
        mounted(){
            if(typeof ResizeObserver!=='undefined'){
                this._observer=new ResizeObserver(()=>this.schedule());
                this._observer.observe(this.$el);
            }
            this.$el.addEventListener('scroll',this.onScroll,{passive:true});
            this.$el.addEventListener('wheel',this.interact,{passive:true});
            this.$el.addEventListener('touchstart',this.interact,{passive:true});
            this.$el.addEventListener('keydown',this.interact);
            this.$el.addEventListener('load',this.onResize,true);
            window.addEventListener('resize',this.onResize);
            this.schedule();
        },
        beforeDestroy(){
            this._dead=true; cancelAnimationFrame(this._frame);cancelAnimationFrame(this._settle);
            if(this._observer){this._observer.disconnect();}
            this.$el.removeEventListener('scroll',this.onScroll);
            this.$el.removeEventListener('wheel',this.interact);
            this.$el.removeEventListener('touchstart',this.interact);
            this.$el.removeEventListener('keydown',this.interact);
            this.$el.removeEventListener('load',this.onResize,true);
            window.removeEventListener('resize',this.onResize);
            this._sizes.clear(); this._observed.clear();
        },
        watch:{
            items(){
                const keys=new Set(this.items.map(this.key));
                for(const key of this._sizes.keys()){if(!keys.has(key)){this._sizes.delete(key);}}
                this.schedule();
            },
            older(){this.schedule();},
            newer(){this.schedule();},
        },
        updated(){this.schedule();},
        methods:{
            key(item){
                if(item.id){return 'm'+item.id;}
                if(!this._keys.has(item)){this._keys.set(item,'n'+(++this._sequence));}
                return this._keys.get(item);
            },
            heights(){return this.items.map(item=>this._sizes.get(this.key(item))||80);},
            rows(){return Array.from(this.$el.querySelectorAll('[data-chat-row]'));},
            visibleID(){
                const box=this.$el.getBoundingClientRect(); let id=0;
                for(const row of this.rows()){
                    const rect=row.getBoundingClientRect();
                    if(rect.bottom>box.top && rect.top<box.bottom){id=Math.max(id,Number(row.dataset.messageId)||0);}
                }
                return id;
            },
            remember(){
                const box=this.$el.getBoundingClientRect();
                const rows=this.rows();
                const row=rows.find(row=>{const rect=row.getBoundingClientRect();return rect.top>=box.top && rect.top<box.bottom;}) || rows.find(row=>{const rect=row.getBoundingClientRect();return rect.bottom>box.top && rect.top<box.bottom;});
                if(row){this._anchor={key:row.dataset.chatRow,offset:row.getBoundingClientRect().top-box.top};return;}
                const heights=this.heights(),header=this.older?32:0;
                const range=visibleRange(heights,Math.max(0,this.$el.scrollTop-header),this.$el.clientHeight,0);
                const item=this.items[range.start];
                this._anchor=item?{key:this.key(item),offset:this.$el.clientTop+header+range.top-this.$el.scrollTop}:null;
            },
            interact(event){
                if(event.type==='keydown' && !['ArrowUp','ArrowDown','PageUp','PageDown','Home','End',' '].includes(event.key)){return;}
                cancelAnimationFrame(this._settle);this._restoring=false;
            },
            request(direction){
                if(this.loading){return;}
                if(direction!=='latest'){this._restoring=false;this._bottom=false;this.remember();}
                this.$emit(direction);
            },
            onResize(){this.schedule();},
            onScroll(){
                if(this._dead || this._restoring){return;}
                this._bottom=this.$el.scrollHeight-this.$el.clientHeight-this.$el.scrollTop<32 && !this.newer;
                this.remember(); this.updateRange(); this.publish();
                const top=this.$el.scrollTop<60;
                const bottom=this.$el.scrollHeight-this.$el.clientHeight-this.$el.scrollTop<60;
                const edge=(top && this.older?'older:':bottom && this.newer?'newer:':'')+this.items.map(this.key).join(',');
                if(!top && !bottom){this._edge='';}
                if(!this.loading && this.items.length && edge!==this._edge){
                    if(top && this.older){this._edge=edge;this.request('older');}
                    else if(bottom && this.newer){this._edge=edge;this.request('newer');}
                }
            },
            updateRange(){
                const next=visibleRange(this.heights(),Math.max(0,this.$el.scrollTop-(this.older?32:0)),this.$el.clientHeight||600);
                if(Object.keys(next).some(key=>next[key]!==this.range[key])){this.range=next;return true;}
                return false;
            },
            schedule(){
                if(this._dead || this._frame){return;}
                this._frame=requestAnimationFrame(()=>{this._frame=0;this.measure();});
            },
            measure(){
                if(this._dead || !this.$el.clientHeight){return;}
                cancelAnimationFrame(this._settle);this._restoring=true;
                const rows=this.rows();
                const current=new Set(rows);
                if(this._observer){
                    for(const row of this._observed){if(!current.has(row)){this._observer.unobserve(row);}}
                    for(const row of current){if(!this._observed.has(row)){this._observer.observe(row);}}
                    this._observed=current;
                }
                for(const row of rows){this._sizes.set(row.dataset.chatRow,Math.max(1,row.getBoundingClientRect().height));}
                const heights=this.heights();
                if(this._bottom){
                    this.$el.scrollTop=this.$el.scrollHeight;
                }else if(this._anchor){
                    const index=this.items.findIndex(item=>this.key(item)===this._anchor.key);
                    if(index>=0){this.$el.scrollTop=this.$el.clientTop+(this.older?32:0)+heights.slice(0,index).reduce((a,b)=>a+b,0)-this._anchor.offset;}
                }
                if(this.updateRange()){return;}
                this._settle=requestAnimationFrame(()=>{
                    this._restoring=false;this.remember();this.publish();
                });
            },
            publish(){this.$emit('position',{bottom:this._bottom,lastVisibleId:this.visibleID()});},
            toBottom(){this._bottom=true;this._anchor=null;this._restoring=true;this.schedule();},
        },
        render(h){
            const button=(text,event)=>h('button',{attrs:{type:'button',disabled:this.loading},class:['chat-window-action',event==='latest'?'chat-window-latest':''],on:{click:()=>this.request(event)}},[text]);
            const nodes=[];
            if(this.older){nodes.push(button('加载更早消息','older'));}
            nodes.push(h('div',{style:{height:this.range.top+'px'},attrs:{'aria-hidden':'true'}}));
            for(const item of this.items.slice(this.range.start,this.range.end)){
                nodes.push(h('div',{key:this.key(item),class:'chat-window-row',attrs:{'data-chat-row':this.key(item),'data-message-id':item.id||''}},this.$scopedSlots.default({item})));
            }
            nodes.push(h('div',{style:{height:this.range.bottom+'px'},attrs:{'aria-hidden':'true'}}));
            if(this.newer){nodes.push(button('加载较新消息','newer'),button('回到最新消息','latest'));}
            return h('div',{class:'chat-window',attrs:{tabindex:0,'aria-label':'聊天记录','data-message-count':this.items.length}},nodes);
        },
    };
    const api={retain,visibleRange,component};
    if(typeof module!=='undefined' && module.exports){module.exports=api;}
    root.ChatWindow=api;
    if(root.Vue){root.Vue.component('chat-window',component);}
})(typeof window!=='undefined'?window:globalThis);
