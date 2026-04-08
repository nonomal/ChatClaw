<p align="center">
<img src="../../frontend/src/assets/images/logo-floatingball.png" width="150" height="150">
</p>

<h1 align="center">ChatClaw</h1>

<p align="center">
  <strong>Obtenha um agente AI pessoal como o OpenClaw em 5 minutos. Segurança Sandbox, pequeno e rápido</strong>
</p>

<p align="center">
  <a href="../../README.md">English</a> |
  <a href="README_zh-CN.md">简体中文</a> |
  <a href="README_zh-TW.md">繁體中文</a> |
  <a href="README_ja-JP.md">日本語</a> |
  <a href="README_ko-KR.md">한국어</a> |
  <a href="README_ar-SA.md">العربية</a> |
  <a href="README_bn-BD.md">বাংলা</a> |
  <a href="README_de-DE.md">Deutsch</a> |
  <a href="README_es-ES.md">Español</a> |
  <a href="README_fr-FR.md">Français</a> |
  <a href="README_hi-IN.md">हिन्दी</a> |
  <a href="README_it-IT.md">Italiano</a> |
  <a href="README_pt-BR.md">Português</a> |
  <a href="README_sl-SI.md">Slovenščina</a> |
  <a href="README_tr-TR.md">Türkçe</a> |
  <a href="README_vi-VN.md">Tiếng Việt</a>
</p>

Obtenha um agente AI pessoal como o OpenClaw em 5 minutos. Seguro com Sandbox, com um instalador ultra-pequeno de 30MB para macOS e Windows (instala em 1 minuto). Conecta-se ao WhatsApp, Telegram, Slack, Discord, Gmail, DingTalk, WeChat Work, QQ, Feishu e outros aplicativos de mensagens. Mercado de Habilidades integrado, Base de Conhecimento, Memória, MCP, Tarefas Agendadas. Desenvolvido em Go: rápido e baixo consumo de recursos.

## Visualizações

### Assistente de Chat AI
Faça qualquer pergunta ao seu assistente de IA; ele pesquisará intelligentemente na sua base de conhecimentos para gerar uma resposta relevante.
![](../../images/previews/en/image1.png)

### Troca de modo duplo para manipulação eficiente de tarefas
O modo Chat se adapta a perguntas e respostas e raciocínio em múltiplos cenários; o modo Tarefa é combinado com um mercado de habilidades integrado, permitindo que agentes de IA decompõem e avançam tarefas de múltiplas etapas de forma autônoma para melhorar a eficiência.
![](../../images/previews/en/image2.png)

### Geração Rápida de PPT
Envie um comando de uma frase ao assistente inteligente para criar e gerar automaticamente uma apresentação PowerPoint.
![](../../images/previews/en/image3.png)

### Gerenciador de Habilidades
Use um comando para que o assistente o ajude a encontrar funcionalidades instaladas no seu computador ou instalar novos plugins de extensão. Mercado de Habilidades — navegue e instale habilidades livremente.
![](../../images/previews/en/image4.png)

### Memória: Interação Mais Natural e Mais Inteligente
Habilite conversas contextuais e assistência personalizada. Aprendizado contínuo e evolução — o assistente parece um parceiro em crescimento que oferece um serviço cada vez mais atencioso e inteligente.
![](../../images/previews/en/image5.png)

### Teste Gratuito de Modelo — Base de Conhecimento Compartilhada da Equipe
Autorização com um clique para conectar ao ChatWiki, sincronizar créditos da conta ChatWiki e suportar modelos personalizados. LLMs nacionais e internacionais de alta qualidade integrados, incluindo Ollama, Google Gemini e OpenAI — use seu modelo de IA favorito para trabalho de escritório diário ou cenários profissionais.
![](../../images/previews/en/image6.png)

### Base de Conhecimento | Armazenamento de Vetorização de Documentos
Carregue seus documentos (TXT, PDF, Word, Excel, CSV, HTML, Markdown). O sistema os analisa, divide e converte automaticamente em vetores de incorporação, armazenados na sua base de conhecimentos privada para recuperação e utilização precisas por modelos de IA.
![](../../images/previews/en/image7.png)

### Integrações de Canais IM
Por meio da integração de SDKs fornecidos por fornecedores de mensagens instantâneas (Feishu, WeCom, QQ, DingTalk, WeChat, WhatsApp e mais), implemente rapidamente capacidades completas de comunicação IM no aplicativo, incluindo criação de canais, gerenciamento de usuários e envio/recebimento de mensagens.
![](../../images/previews/en/image8.png)

### Tarefas Agendadas — Execução Automática de Comandos
Deixe o assistente executar automaticamente operações específicas em horários ou intervalos predefinidos, como fornecer lembretes oportunos, executar trabalhos recorrentes e realizar tarefas de manutenção em nível de sistema.
![](../../images/previews/en/image9.png)

### Seleção de Texto para Q&A Instantâneo
Selecione qualquer texto na tela e ele será automaticamente copiado para uma caixa de pergunta rápida flutuante. Um clique para enviar ao assistente de IA e obter uma resposta instantânea.
![](../../images/previews/en/image10.png)

### Barra Lateral Inteligente
Um assistente inteligente que pode ser encaixado ao lado de outras janelas de aplicativos. Alterne rapidamente entre assistentes de IA configurados de forma diferente para fazer perguntas. O robô gera respostas baseadas na sua base de conhecimentos associada, e suporta o envio de respostas com um clique nas suas conversas.
![](../../images/previews/en/image11.png)

### Uma Pergunta, Múltiplas Respostas: Compare com Facilidade
Não precisa repetir perguntas. Consulte vários "especialistas em IA" simultaneamente e veja suas respostas lado a lado na mesma interface. Fácil de comparar e ajuda você a chegar à melhor conclusão.
![](../../images/previews/en/image12.png)

### Bola de Inicialização com Um Clique
Clique na esfera flutuante na sua área de trabalho para despertar ou abrir instantaneamente a janela principal do aplicativo ChatClaw.
![](../../images/previews/en/image13.png)

## Implantação em Modo Servidor

ChatClaw pode ser executado como servidor (sem necessidade de GUI de desktop), acessível através do navegador.

### Binário Direto

Baixe o binário para sua plataforma em [GitHub Releases](https://github.com/chatwiki/chatclaw/releases):

|| Plataforma | Arquivo |
||----------|------|
|| Linux x86_64 | `ChatClaw-server-linux-amd64` |
|| Linux ARM64 | `ChatClaw-server-linux-arm64` |

```bash
chmod +x ChatClaw-server-linux-amd64
./ChatClaw-server-linux-amd64
```

Abra http://localhost:8080 no seu navegador.

O servidor ouve em `0.0.0.0:8080` por padrão. Você pode personalizar host e porta através de variáveis de ambiente:

```bash
WAILS_SERVER_HOST=127.0.0.1 WAILS_SERVER_PORT=3000 ./ChatClaw-server-linux-amd64
```

### Docker

```bash
docker run -d \
  --name chatclaw-server \
  -p 8080:8080 \
  -v chatclaw-data:/root/.config/chatclaw \
  registry.cn-hangzhou.aliyuncs.com/chatwiki/chatclaw:latest
```

Abra http://localhost:8080 no seu navegador.

### Docker Compose

Crie um arquivo `docker-compose.yml`:

```yaml
services:
  chatclaw:
    image: registry.cn-hangzhou.aliyuncs.com/chatwiki/chatclaw:latest
    container_name: chatclaw-server
    volumes:
      - chatclaw-data:/root/.config/chatclaw
    ports:
      - "8080:8080"
    restart: unless-stopped

volumes:
  chatclaw-data:
```

Então execute:

```bash
docker compose up -d
```

Abra http://localhost:8080 no seu navegador. Para parar: `docker compose down`. Os dados persistem no volume `chatclaw-data`.

## Stack Tecnológico

|| Camada | Tecnologia |
||-------|-----------|
|| Framework Desktop | [Wails v3](https://wails.io/) (Go + WebView) |
|| Linguagem Backend | [Go 1.26](https://go.dev/) |
|| Framework Frontend | [Vue 3](https://vuejs.org/) + [TypeScript](https://www.typescriptlang.org/) |
|| Componentes UI | [shadcn-vue](https://www.shadcn-vue.com/) + [Reka UI](https://reka-ui.com/) |
|| Estilização | [Tailwind CSS v4](https://tailwindcss.com/) |
|| Gerenciamento de Estado | [Pinia](https://pinia.vuejs.org/) |
|| Ferramenta de Build | [Vite](https://vite.dev/) |
|| Framework AI | [Eino](https://github.com/cloudwego/eino) (ByteDance CloudWeGo) |
|| Fornecedores de Modelos AI | OpenAI / Claude / Gemini / Ollama / DeepSeek / Doubao / Qwen / Zhipu / Grok |
|| Banco de Dados | [SQLite](https://www.sqlite.org/) + [sqlite-vec](https://github.com/asg017/sqlite-vec) (busca vetorial) |
|| Internacionalização | [go-i18n](https://github.com/nicksnyder/go-i18n) + [vue-i18n](https://vue-i18n.intlify.dev/) |
|| Executor de Tarefas | [Task](https://taskfile.dev/) |
|| Ícones | [Lucide](https://lucide.dev/) |

## Estrutura do Projeto

```
ChatClaw_D2/
├── main.go                     # Ponto de entrada do aplicativo
├── go.mod / go.sum             # Dependências do módulo Go
├── Taskfile.yml                # Configuração do executor de tarefas
├── build/                      # Configurações de build e assets de plataforma
│   ├── config.yml              # Configuração de build Wails
│   ├── darwin/                 # Configurações de build macOS e entitlements
│   ├── windows:                # Instalador Windows (NSIS/MSIX) e manifestos
│   ├── linux:                  # Empacotamento Linux (AppImage, nfpm)
│   ├── ios:                    # Configurações de build iOS
│   └── android:                # Configurações de build Android
├── frontend:                   # Aplicativo frontend Vue 3
│   ├── package.json            # Dependências Node.js
│   ├── vite.config.ts          # Configuração do bundler Vite
│   ├── components.json         # Configuração shadcn-vue
│   ├── index.html              # Entry da janela principal
│   ├── floatingball.html       # Entry da janela bola flutuante
│   ├── selection.html          # Entry do popup de seleção de texto
│   ├── winsnap.html            # Entry da janela Snap
│   └── src/
│       ├── assets:             # Ícones (SVG), imagens e CSS global
│       ├── components:         # Componentes compartilhados
│       │   ├── layout:         # Layout do app, barra lateral, barra de título
│       │   └── ui:             # Primitivos shadcn-vue (button, dialog, toast…)
│       ├── composables:        # Composables Vue (lógica reutilizável)
│       ├── i18n:               # Setup i18n do frontend
│       ├── locales:            # Arquivos de tradução (zh-CN, en-US…)
│       ├── lib:                # Funções utilitárias
│       ├── pages:              # Visualizações no nível de página
│       │   ├── assistant:      # Página do assistente de chat AI e componentes
│       │   ├── knowledge:      # Página de gerenciamento de base de conhecimento
│       │   ├── multiask:       # Página de comparação multi-modelo
│       │   └── settings:       # Página de configurações (fornecedores, modelos, ferramentas…)
│       ├── stores:             # Stores de estado Pinia
│       ├── floatingball:       # Mini-app bola flutuante
│       ├── selection:          # Mini-app seleção de texto
│       └── winsnap:            # Mini-app janela Snap
├── internal:                   # Pacotes Go privados
│   ├── bootstrap:              # Inicialização do aplicativo e fiação
│   ├── define:                 # Constantes, fornecedores integrados, flags de ambiente
│   ├── device:                 # Identificação de dispositivo
│   ├── eino:                   # Camada de integração AI/LLM
│   │   ├── agent:              # Orquestração de Agente
│   │   ├── chatmodel:          # Fábrica de modelos de chat (multi-fornecedor)
│   │   ├── embedding:          # Fábrica de modelos de embedding
│   │   ├── filesystem:         # Ferramentas de sistema de arquivos para Agente AI
│   │   ├── parser:             # Parsers de documentos (PDF, DOCX, XLSX, CSV)
│   │   ├── processor:          # Pipeline de processamento de documentos
│   │   ├── raptor:             # Resumo recursivo RAPTOR
│   │   ├── splitter:           # Fábrica de divisores de texto
│   │   └── tools:              # Integrações de ferramentas AI (navegador, pesquisa, calculadora…)
│   ├── errs:                   # Tratamento de erros i18n-aware
│   ├── fts:                    # Tokenizer de busca em texto completo
│   ├── logger:                 # Logging estruturado
│   ├── services:               # Serviços de lógica de negócio
│   │   ├── agents:             # CRUD de Agente
│   │   ├── app:                # Ciclo de vida do aplicativo
│   │   ├── browser:            # Automação de navegador (chromedp)
│   │   ├── chat:               # Chat e streaming
│   │   ├── conversations:      # Gerenciamento de conversas
│   │   ├── document:           # Upload de documentos e vetorização
│   │   ├── floatingball:       # Janela bola flutuante (cross-platform)
│   │   ├── i18n:               # i18n do backend
│   │   ├── library:            # CRUD de biblioteca de conhecimento
│   │   ├── multiask:           # Q&A multi-modelo
│   │   ├── providers:          # Configuração de fornecedor AI
│   │   ├── retrieval:          # Serviço de retrieval RAG
│   │   ├── settings:           # Configurações de usuário com cache
│   │   ├── textselection:      # Seleção de texto na tela (cross-platform)
│   │   ├── thumbnail:          # Captura de miniatura de janela
│   │   ├── tray:               # Bandeja do sistema
│   │   ├── updater:            # Atualização automática (GitHub/Gitee)
│   │   ├── windows:            # Gerenciamento de janelas e serviço Snap
│   │   └── winsnapchat:        # Serviço de sessão de chat Snap
│   ├── sqlite:                 # Camada de banco de dados (Bun ORM + migrações)
│   └── taskmanager:            # Agendador de tarefas em segundo plano
├── pkg:                         # Pacotes Go públicos/reutilizáveis
│   ├── webviewpanel:           # Gerenciador de painel WebView cross-platform
│   ├── winsnap:                # Motor de snap de janelas (macOS/Windows/Linux)
│   └── winutil:                # Utilitários de ativação de janela
├── docs:                       # Documentação de desenvolvimento
└── images:                      # Capturas de tela do README
```

