// Package operationscompressor ports vendor
// app/js/OperationsCompressor.js (OperationsCompressor.compressOperations).
package operationscompressor

import "ollitex/go/services/project-history/internal/historyot"

// CompressOperations — vendor `compressOperations(operations)`: fold
// consecutive operations that `canBeComposedWith` into one, preserving
// order.
func CompressOperations(operations []historyot.Operation) []historyot.Operation {
	if len(operations) == 0 {
		return []historyot.Operation{}
	}

	newOperations := []historyot.Operation{}
	currentOperation := operations[0]
	for operationId := 1; operationId < len(operations); operationId++ {
		nextOperation := operations[operationId]
		if currentOperation.CanBeComposedWith(nextOperation) {
			composed, err := currentOperation.Compose(nextOperation)
			if err != nil {
				// Vendor: `canBeComposedWith` is a precondition for
				// `compose`; reaching here means the model is broken.
				panic("OperationsCompressor: compose failed after canBeComposedWith")
			}
			currentOperation = composed
		} else {
			// currentOperation and nextOperation cannot be composed. Push
			// the currentOperation and start over with nextOperation.
			newOperations = append(newOperations, currentOperation)
			currentOperation = nextOperation
		}
	}
	newOperations = append(newOperations, currentOperation)

	return newOperations
}
