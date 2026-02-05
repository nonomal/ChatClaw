package splitter

import (
	"context"

	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/recursive"
	"github.com/cloudwego/eino-ext/components/document/transformer/splitter/semantic"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/components/embedding"
)

// Config 创建分割器的配置
type Config struct {
	// ChunkSize 每个分块的目标大小（按字符数计算）
	ChunkSize int
	// ChunkOverlap 相邻分块之间的重叠大小（按字符数计算）
	ChunkOverlap int
	// SemanticEmbedder 语义分割使用的嵌入模型（可选）
	// 如果提供，将使用语义分割而非递归分割
	SemanticEmbedder embedding.Embedder
	// SemanticPercentile 语义分割的百分位阈值（0-1）
	// 值越高，分割点越少。默认为 0.9
	SemanticPercentile float64
	// SemanticMinChunkSize 语义分割的最小分块大小
	// 默认为 100
	SemanticMinChunkSize int
}

// DefaultSeparators 递归分割的默认分隔符
var DefaultSeparators = []string{
	"\n\n", // 段落分隔
	"\n",   // 换行符
	"。",   // 中文句号
	"！",   // 中文感叹号
	"？",   // 中文问号
	"；",   // 中文分号
	".",    // 英文句号
	"!",    // 英文感叹号
	"?",    // 英文问号
	";",    // 英文分号
	" ",    // 空格
	"",     // 逐字符分割
}

// NewSplitter 根据配置创建新的文档分割器
// 如果提供了 SemanticEmbedder，则创建语义分割器
// 否则创建递归分割器
func NewSplitter(ctx context.Context, cfg *Config) (document.Transformer, error) {
	if cfg == nil {
		cfg = &Config{
			ChunkSize:    1024,
			ChunkOverlap: 100,
		}
	}

	// 使用字符长度计算（适用于中文）
	lenFunc := func(s string) int {
		return len([]rune(s))
	}

	// 如果提供了语义嵌入模型，使用语义分割
	if cfg.SemanticEmbedder != nil {
		percentile := cfg.SemanticPercentile
		if percentile <= 0 || percentile > 1 {
			percentile = 0.9
		}
		minChunkSize := cfg.SemanticMinChunkSize
		if minChunkSize <= 0 {
			minChunkSize = 100
		}

		return semantic.NewSplitter(ctx, &semantic.Config{
			Embedding:    cfg.SemanticEmbedder,
			BufferSize:   2,
			MinChunkSize: minChunkSize,
			Separators:   DefaultSeparators,
			Percentile:   percentile,
			LenFunc:      lenFunc,
		})
	}

	// 默认使用递归分割
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1024
	}
	chunkOverlap := cfg.ChunkOverlap
	if chunkOverlap < 0 {
		chunkOverlap = 100
	}

	return recursive.NewSplitter(ctx, &recursive.Config{
		ChunkSize:   chunkSize,
		OverlapSize: chunkOverlap,
		Separators:  DefaultSeparators,
		LenFunc:     lenFunc,
		KeepType:    recursive.KeepTypeEnd,
	})
}

// NewRecursiveSplitter 使用给定配置创建递归分割器
func NewRecursiveSplitter(ctx context.Context, chunkSize, chunkOverlap int) (document.Transformer, error) {
	return NewSplitter(ctx, &Config{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	})
}

// NewSemanticSplitter 使用给定的嵌入模型创建语义分割器
func NewSemanticSplitter(ctx context.Context, embedder embedding.Embedder, percentile float64) (document.Transformer, error) {
	return NewSplitter(ctx, &Config{
		SemanticEmbedder:   embedder,
		SemanticPercentile: percentile,
	})
}
